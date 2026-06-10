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

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/config"
	"github.com/axsh/hag/llmgateway"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/tasklog"
)

// AgentService is the interface for the Coding Agent API service.
type AgentService interface {
	HTTPHandler() http.Handler
}

// Server is the Coding Agent API service layer.
type Server struct {
	agents         map[string]codingagent.CodingAgent
	sessions       codingagent.SessionStore
	logger         logger.Logger
	taskLog        *tasklog.TaskLog
	gatewayURL     string
	cliVersions    map[string]string        // cached at init
	gatewayModels  []llmgateway.ModelInfo   // cached model list from LLMGP
	gatewayDefault *llmgateway.ModelInfo    // cached default model from LLMGP
	profiles       *config.ModelProfilesConfig // for logical name resolution
	httpServer     *http.Server
	ln             net.Listener
	port           int // actual listen port (set after Launch)
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithLogger sets the logger for the server.
func WithLogger(log logger.Logger) ServerOption {
	return func(s *Server) { s.logger = log }
}

// WithTaskLog sets the TaskLog for the server.
func WithTaskLog(tl *tasklog.TaskLog) ServerOption {
	return func(s *Server) { s.taskLog = tl }
}

// WithGatewayURL sets the LLM Gateway Proxy URL for health checks.
func WithGatewayURL(url string) ServerOption {
	return func(s *Server) { s.gatewayURL = url }
}

// New creates a new AgentService Server.
func New(opts ...ServerOption) *Server {
	s := &Server{
		agents:   make(map[string]codingagent.CodingAgent),
		sessions: NewMemorySessionStore(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewWithStore creates a Server with a custom SessionStore (for testing).
func NewWithStore(store codingagent.SessionStore, opts ...ServerOption) *Server {
	s := &Server{
		agents:   make(map[string]codingagent.CodingAgent),
		sessions: store,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterAgent registers a CodingAgent with the server.
func (s *Server) RegisterAgent(agent codingagent.CodingAgent) {
	s.agents[agent.Name()] = agent
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
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.Error("agentservice serve error", "error", err)
			}
		}
	}()
	if s.logger != nil {
		s.logger.Info("agentservice started", "port", s.port)
	}
	return nil
}

// Shutdown gracefully stops the AgentService HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("shutting down agentservice")
	}
	// Close all active coding agent processes.
	for _, agent := range s.agents {
		if closer, ok := agent.(interface{ Close() error }); ok {
			closer.Close()
		}
	}
	return s.httpServer.Shutdown(ctx)
}

// Port returns the actual port the server is listening on.
// Returns 0 if the server has not been launched.
func (s *Server) Port() int {
	return s.port
}

// HTTPHandler returns the HTTP handler with all endpoint routes.
// CLI versions are detected lazily here, after all agents are registered.
func (s *Server) HTTPHandler() http.Handler {
	if s.cliVersions == nil {
		s.cliVersions = detectCLIVersions(s.agents, s.logger)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/agents", s.routeAgents)
	mux.HandleFunc("/api/v1/models", s.routeModels)
	mux.HandleFunc("/api/v1/sessions", s.routeSessions)
	mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)
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
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) routeSessionByID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/messages") {
		s.handleSendMessage(w, r)
	} else if strings.HasSuffix(path, "/terminate") {
		s.handleTerminate(w, r)
	} else if strings.HasSuffix(path, "/logs") {
		s.handleLogStream(w, r)
	} else {
		switch r.Method {
		case http.MethodGet:
			s.handleGetSession(w, r)
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

		// R8: Validate CLI version meets minimum requirement.
		if verErr := checkCLIVersion(versionStr, minClaudeCLIVersion); verErr != nil {
			if log != nil {
				log.Error(verErr.Error(), "agent", agentName)
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
		for _, key := range prov.Keys {
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
