package agentservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	artifactapi "github.com/axsh/arctic-tern/shared/libs/go/artifact/api"
	artifactstorage "github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
)

// AgentService is the interface for the Coding Agent API service.
type AgentService interface {
	HTTPHandler() http.Handler
}

// Server is the Coding Agent API service layer.
type Server struct {
	agents            map[string]codingagent.CodingAgent
	sessions          codingagent.SessionStore
	logger            logger.Logger
	taskLog           *tasklog.TaskLog
	gatewayURL        string
	gatewayToken      string
	cliVersions       map[string]string           // cached at init
	gatewayModels     []llmgateway.ModelInfo      // cached model list from LLMGP
	gatewayDefault    *llmgateway.ModelInfo       // cached default model from LLMGP
	profiles          *config.ModelProfilesConfig // for logical name resolution
	httpServer        *http.Server
	ln                net.Listener
	port              int // actual listen port (set after Launch)
	activeMu          sync.Mutex
	activeSessions    map[string]codingagent.Session
	execCancelMu      sync.Mutex
	execCancels       map[string]context.CancelFunc // sessionID -> execution cancel
	execRegistry      *execRegistry
	enabledVersions   map[int]bool // API versions to register
	disableSandbox    bool
	enableSubagent    bool
	lastGatewayHealth GatewayHealth
	gatewayHealthMu   sync.Mutex
	pollCancel        context.CancelFunc
	// Artifact support (optional; nil disables artifact tracking and API).
	artifactStore      store.ArtifactStore
	artifactStorage    *artifactstorage.UserArtifactStorage
	artifactWorkDir    string
	sessionSnapshots   map[string]analyzer.DirSnapshot
	sessionSnapshotsMu sync.Mutex
	summarizer         portable.Summarizer
	supplementCfg      config.SupplementConfig
	processRetry       config.ProcessRetryConfig
	processRetryCustom bool
	sseDrainTimeout    time.Duration
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithLogger sets the logger for the server.
func WithLogger(log logger.Logger) ServerOption {
	return func(s *Server) { s.logger = log.WithComponent("agentservice") }
}

// WithTaskLog sets the TaskLog for the server.
func WithTaskLog(tl *tasklog.TaskLog) ServerOption {
	return func(s *Server) { s.taskLog = tl }
}

// WithGatewayURL sets the LLM Gateway Proxy URL for health checks.
func WithGatewayURL(url string) ServerOption {
	return func(s *Server) { s.gatewayURL = url }
}

// WithGatewayToken sets the LLM Gateway Proxy authentication token.
func WithGatewayToken(token string) ServerOption {
	return func(s *Server) { s.gatewayToken = token }
}

// WithSandboxDisabled configures whether sandbox is disabled.
func WithSandboxDisabled(disabled bool) ServerOption {
	return func(s *Server) { s.disableSandbox = disabled }
}

// WithSubagentEnabled configures whether subagent is enabled.
func WithSubagentEnabled(enabled bool) ServerOption {
	return func(s *Server) { s.enableSubagent = enabled }
}

// WithArtifactStore attaches an ArtifactStore (and the ToolCallAnalyzer) to the server.
// workDir is the project root used to convert absolute paths to relative keys.
// If s is nil, artifact tracking and the /api/v1/artifacts/system routes are disabled.
func WithArtifactStore(s store.ArtifactStore, workDir string) ServerOption {
	return func(srv *Server) {
		srv.artifactStore = s
		srv.artifactWorkDir = workDir
	}
}

// WithArtifactStorage attaches a UserArtifactStorage to the server, enabling the
// /api/v1/artifacts/user routes and MCP tool registration.
func WithArtifactStorage(st *artifactstorage.UserArtifactStorage) ServerOption {
	return func(srv *Server) {
		srv.artifactStorage = st
	}
}

// WithSupplementConfig sets the server-default supplement strategy.
func WithSupplementConfig(cfg config.SupplementConfig) ServerOption {
	return func(s *Server) { s.supplementCfg = cfg }
}

// WithSummarizer injects a portable.Summarizer (tests replace GatewaySummarizer).
func WithSummarizer(sum portable.Summarizer) ServerOption {
	return func(s *Server) { s.summarizer = sum }
}

// WithProcessRetry sets Codex process re-exec bounds. IntervalSeconds 0 means no wait.
func WithProcessRetry(cfg config.ProcessRetryConfig) ServerOption {
	return func(s *Server) {
		s.processRetry = cfg
		s.processRetryCustom = true
	}
}

// WithSSEDrainTimeout overrides the post-disconnect reattach bound for tests.
// Production zero value uses defaultSSEClientDrainTimeout (90s).
func WithSSEDrainTimeout(d time.Duration) ServerOption {
	return func(s *Server) { s.sseDrainTimeout = d }
}

// MarkSessionBusy registers a dummy execution so PATCH/SendMessage return 409 (tests).
func (s *Server) MarkSessionBusy(sessionID, status string) error {
	return s.execRegistry.Register(sessionID, &activeExecution{
		sessionID: sessionID,
		status:    status,
		turnID:    "busy-turn",
		relay:     &eventRelay{notify: make(chan struct{}, 1), sourceDone: true},
	})
}

func applyProcessRetryDefaults(s *Server) {
	if s.processRetry.MaxAttempts == 0 {
		s.processRetry.MaxAttempts = 3
	}
	if !s.processRetryCustom && s.processRetry.IntervalSeconds == 0 {
		s.processRetry.IntervalSeconds = 3
	}
}

// New creates a new AgentService Server.
func New(opts ...ServerOption) *Server {
	s := &Server{
		agents:         make(map[string]codingagent.CodingAgent),
		sessions:       NewWorkspaceSessionStore(),
		activeSessions: make(map[string]codingagent.Session),
		execCancels:    make(map[string]context.CancelFunc),
		execRegistry:   newExecRegistry(),
	}
	for _, opt := range opts {
		opt(s)
	}
	applyProcessRetryDefaults(s)
	if s.summarizer == nil {
		s.summarizer = &GatewaySummarizer{
			GatewayURL: s.gatewayURL,
			Token:      s.gatewayToken,
		}
	}
	if s.logger != nil {
		s.logger.Debug("creating agent service", "agent_count", len(s.agents))
	}
	// Attach ToolCallAnalyzer when an ArtifactStore is provided.
	if s.artifactStore != nil && s.taskLog != nil {
		analyzer.New(s.taskLog, s.artifactStore, s.artifactWorkDir, func(sessionID string) string {
			if rec, err := s.sessions.Get(sessionID); err == nil {
				return rec.WorkDir
			}
			return ""
		})
		if s.logger != nil {
			s.logger.Debug("artifact tracking enabled", "work_dir", s.artifactWorkDir)
		}
	}
	return s
}

// NewWithStore creates a Server with a custom SessionStore (for testing).
func NewWithStore(store codingagent.SessionStore, opts ...ServerOption) *Server {
	s := &Server{
		agents:         make(map[string]codingagent.CodingAgent),
		sessions:       store,
		activeSessions: make(map[string]codingagent.Session),
		execCancels:    make(map[string]context.CancelFunc),
		execRegistry:   newExecRegistry(),
	}
	for _, opt := range opts {
		opt(s)
	}
	applyProcessRetryDefaults(s)
	if s.summarizer == nil {
		s.summarizer = &GatewaySummarizer{
			GatewayURL: s.gatewayURL,
			Token:      s.gatewayToken,
		}
	}
	if s.logger != nil {
		s.logger.Debug("creating agent service", "agent_count", len(s.agents))
	}
	return s
}

// RegisterAgent registers a CodingAgent with the server.
func (s *Server) RegisterAgent(agent codingagent.CodingAgent) {
	if s.gatewayToken != "" {
		if inj, ok := agent.(interface{ SetGatewayToken(string) }); ok {
			inj.SetGatewayToken(s.gatewayToken)
		}
	}
	s.agents[agent.Name()] = agent
	if s.logger != nil {
		s.logger.Debug("agent registered", "agent_name", agent.Name())
	}
}

// Launch starts the AgentService HTTP server on the given port.
// port=0 uses OS-assigned ephemeral port. Non-blocking.
func (s *Server) Launch(ctx context.Context, port int) error {
	handler := s.HTTPHandler()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("agentservice listen: %w", err)
	}
	s.ln = ln
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.httpServer = &http.Server{Handler: handler}

	s.gatewayHealthMu.Lock()
	s.lastGatewayHealth = GatewayHealth{
		Status:        "ok",
		URL:           s.gatewayURL,
		LastCheckedAt: time.Now(),
	}
	s.gatewayHealthMu.Unlock()

	pollCtx, cancel := context.WithCancel(context.Background())
	s.pollCancel = cancel
	go s.startGatewayHealthPolling(pollCtx)

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.Error("agentservice serve error", "error", err)
			}
		}
	}()
	if s.logger != nil {
		addr := ""
		if s.ln != nil {
			addr = s.ln.Addr().String()
		}
		s.logger.Info("agent service listening", "port", s.port, "addr", addr)
	}
	return nil
}

// Shutdown gracefully stops the AgentService HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.pollCancel != nil {
		s.pollCancel()
	}

	if s.httpServer == nil {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("agent service shutting down")
	}
	// Close all active coding agent processes.
	for _, agent := range s.agents {
		if closer, ok := agent.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	// Close all active sessions.
	s.activeMu.Lock()
	for id, sess := range s.activeSessions {
		if s.logger != nil {
			s.logger.Debug("closing active session on shutdown", "session_id", id)
		}
		sess.Close()
	}
	s.activeSessions = make(map[string]codingagent.Session)
	s.activeMu.Unlock()

	// Cancel all running agent executions.
	s.execCancelMu.Lock()
	for id, cancel := range s.execCancels {
		if s.logger != nil {
			s.logger.Debug("cancelling execution on shutdown", "session_id", id)
		}
		cancel()
	}
	s.execCancels = make(map[string]context.CancelFunc)
	s.execCancelMu.Unlock()

	return s.httpServer.Shutdown(ctx)
}

func (s *Server) RegisterActiveSession(id string, sess codingagent.Session) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	s.activeSessions[id] = sess
}

func (s *Server) UnregisterActiveSession(id string) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	delete(s.activeSessions, id)
}

// RegisterExecCancel registers a cancel function for an agent execution context.
func (s *Server) RegisterExecCancel(id string, cancel context.CancelFunc) {
	s.execCancelMu.Lock()
	defer s.execCancelMu.Unlock()
	s.execCancels[id] = cancel
}

// UnregisterExecCancel removes a cancel function for an agent execution context.
func (s *Server) UnregisterExecCancel(id string) {
	s.execCancelMu.Lock()
	defer s.execCancelMu.Unlock()
	delete(s.execCancels, id)
}

// CancelExecution cancels the execution context for the given session.
// Returns true if the session was found and cancelled.
func (s *Server) CancelExecution(id string) bool {
	s.execCancelMu.Lock()
	defer s.execCancelMu.Unlock()
	if cancel, ok := s.execCancels[id]; ok {
		cancel()
		return true
	}
	return false
}

// Port returns the actual port the server is listening on.
// Returns 0 if the server has not been launched.
func (s *Server) Port() int {
	return s.port
}

// SetEnabledVersions configures which API versions are active.
func (s *Server) SetEnabledVersions(versions []int) {
	s.enabledVersions = make(map[int]bool)
	for _, v := range versions {
		s.enabledVersions[v] = true
	}
}

// isVersionEnabled checks if a specific API version is enabled.
// Returns true if enabledVersions is empty (all versions enabled by default).
func (s *Server) isVersionEnabled(v int) bool {
	if len(s.enabledVersions) == 0 {
		return true
	}
	return s.enabledVersions[v]
}

// HTTPHandler returns the HTTP handler with all endpoint routes.
// CLI versions are detected lazily here, after all agents are registered.
func (s *Server) HTTPHandler() http.Handler {
	if s.cliVersions == nil {
		s.cliVersions = detectCLIVersions(s.agents, s.logger)
	}

	s.gatewayHealthMu.Lock()
	if s.lastGatewayHealth.LastCheckedAt.IsZero() {
		s.gatewayHealthMu.Unlock()
		s.updateGatewayHealth()
	} else {
		s.gatewayHealthMu.Unlock()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)

	if s.isVersionEnabled(1) {
		mux.HandleFunc("/api/v1/agents", s.routeAgents)
		mux.HandleFunc("/api/v1/models", s.routeModels)
		mux.HandleFunc("/api/v1/embeddings", s.routeEmbeddings)
		mux.HandleFunc("/api/v1/embeddings/models", s.routeEmbeddingModels)
		mux.HandleFunc("/api/v1/sessions", s.routeSessions)
		mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)

		// Register system artifact routes when an ArtifactStore is configured.
		if s.artifactStore != nil {
			artifactapi.NewSystemArtifactHandler(s.artifactStore).
				RegisterRoutes(mux, "/api/v1/artifacts/system")
		}
		// Register user artifact routes when both store and storage are configured.
		if s.artifactStore != nil && s.artifactStorage != nil {
			artifactapi.NewUserArtifactHandler(s.artifactStore, s.artifactStorage).
				RegisterRoutes(mux, "/api/v1/artifacts/user")
		}
	}
	return mux
}

func (s *Server) routeAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListAgents(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) routeModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListModels(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) routeSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateSession(w, r)
	case http.MethodGet:
		s.handleListSessions(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) routeSessionByID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/messages") {
		s.handleSendMessage(w, r)
	} else if strings.HasSuffix(path, "/respond") {
		s.handleRespond(w, r)
	} else if strings.HasSuffix(path, "/terminate") {
		s.handleTerminate(w, r)
	} else if strings.HasSuffix(path, "/logs") {
		s.handleLogStream(w, r)
	} else if strings.HasSuffix(path, "/events") {
		s.handleFollow(w, r)
	} else {
		switch r.Method {
		case http.MethodGet:
			s.handleGetSession(w, r)
		case http.MethodPatch:
			s.handlePatchSession(w, r)
		case http.MethodDelete:
			s.handleDeleteSession(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// generateID generates a unique session ID.
func (s *Server) generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// detectCLIVersions runs "claude --version" / "codex --version" once at init.
// Returns a map of agent name -> version string (or "unavailable").
// Logs an error if a detected version does not meet the minimum requirement.
func detectCLIVersions(agents map[string]codingagent.CodingAgent, log logger.Logger) map[string]string {
	versions := make(map[string]string)
	cliNames := map[string]string{
		"claudecode": "claude",
		"codex":      "codex",
	}
	for agentName := range agents {
		cliName, ok := cliNames[agentName]
		if !ok {
			versions[agentName] = "unavailable"
			continue
		}
		out, err := exec.Command(cliName, "--version").Output()
		if err != nil {
			versions[agentName] = "unavailable"
			continue
		}
		versionStr := strings.TrimSpace(string(out))
		versions[agentName] = versionStr

		// Use the agent-specific parser/validator from factory
		parser := GetVersionParser(agentName)
		if parser != nil {
			if _, _, _, err := parser.Parse(versionStr); err != nil {
				if log != nil {
					log.Error("failed to parse CLI version: "+err.Error(), "agent", agentName)
				}
			} else if verErr := parser.Check(versionStr); verErr != nil {
				if log != nil {
					log.Error(verErr.Error(), "agent", agentName)
				}
			}
		}
	}
	return versions
}

// FetchModelsFromGateway calls LLMGP /v1/models and caches the result.
func (s *Server) FetchModelsFromGateway() error {
	if s.gatewayURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", s.gatewayURL+"/v1/models", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body struct {
		Models       []llmgateway.ModelInfo `json:"models"`
		DefaultModel *llmgateway.ModelInfo  `json:"default_model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	s.gatewayModels = body.Models
	s.gatewayDefault = body.DefaultModel
	return nil
}

// SetGatewayModels sets the cached model list directly (for testing).
func (s *Server) SetGatewayModels(models []llmgateway.ModelInfo, defaultModel *llmgateway.ModelInfo) {
	s.gatewayModels = models
	s.gatewayDefault = defaultModel
}

// IsValidModel checks if a model name exists in the cached model list.
func (s *Server) IsValidModel(model string) bool {
	for _, m := range s.gatewayModels {
		if m.Model == model {
			return true
		}
	}
	return false
}

// AvailableModelNames returns a list of model name strings.
func (s *Server) AvailableModelNames() []string {
	names := make([]string, len(s.gatewayModels))
	for i, m := range s.gatewayModels {
		names[i] = m.Model
	}
	return names
}

// SetModelProfiles sets the model profiles configuration for logical name resolution.
func (s *Server) SetModelProfiles(profiles *config.ModelProfilesConfig) {
	s.profiles = profiles
}

// ResolveModel resolves a logical name or model_id to a model_id.
// Returns (model_id, true) if found, ("", false) otherwise.
func (s *Server) ResolveModel(input string) (string, bool) {
	if s.profiles == nil {
		return "", false
	}
	for _, prov := range s.profiles.Providers {
		for _, key := range prov.ApiKeys {
			for _, model := range key.Models {
				// Match by logical_name first.
				if model.LogicalName != "" && model.LogicalName == input {
					return model.Name, true
				}
				// Match by model_id (name).
				if model.Name == input {
					return model.Name, true
				}
			}
		}
	}
	return "", false
}
