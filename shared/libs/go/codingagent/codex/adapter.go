package codex

import (
	"context"
	"fmt"
	"sync"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/logger"
)

// CodexAdapter is a CodingAgent implementation using the Codex CLI.
type CodexAdapter struct {
	config *codingagent.AdapterConfig
	mu     sync.Mutex
	procs  []*ProcessManager
	logger logger.Logger
}

// compile-time interface compliance check
var _ codingagent.CodingAgent = (*CodexAdapter)(nil)

// New creates a CodexAdapter.
func New(config *codingagent.AdapterConfig) *CodexAdapter {
	log := config.Logger
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	return &CodexAdapter{
		config: config,
		logger: log.WithComponent("codex"),
	}
}

// Name returns "codex".
func (a *CodexAdapter) Name() string { return "codex" }

// CreateSession starts a new Codex session by launching the CLI subprocess.
func (a *CodexAdapter) CreateSession(
	ctx context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	codingagent.ApplyDefaults(cfg, a.config)

	a.logger.Info("creating codex session", "model", cfg.Model, "work_dir", cfg.WorkDir)

	// R2: Determine wire_api from AdapterConfig.ModelMode.
	wireAPI := a.config.ModelMode
	if wireAPI == "" {
		wireAPI = "responses"
	}

	// Build -c key=value config overrides for Codex CLI.
	configOverrides := BuildConfigOverrides(cfg.Model, a.config.GatewayURL, wireAPI)

	// Create CODEX_HOME for session/auth data storage.
	codexHome, err := WriteConfigTOML(cfg.Model, a.config.GatewayURL, wireAPI)
	if err != nil {
		a.logger.Warn("failed to create codex home dir, continuing without it", "error", err.Error())
		codexHome = ""
	}

	ch, pm, err := StartProcess(ctx, a.config, cfg, configOverrides, codexHome)
	if err != nil {
		a.logger.Error("failed to start codex process", "error", err.Error())
		return nil, fmt.Errorf("codex: create session: %w", err)
	}

	a.mu.Lock()
	a.procs = append(a.procs, pm)
	a.mu.Unlock()

	sid := fmt.Sprintf("codex-%d", pm.cmd.Process.Pid)
	a.logger.Info("codex session created", "session_id", sid, "pid", pm.cmd.Process.Pid)
	return &codexSession{id: sid, ch: ch, pm: pm}, nil
}

// Close stops all active processes and releases resources.
func (a *CodexAdapter) Close() error {
	a.logger.Debug("closing all codex sessions")
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, pm := range a.procs {
		pm.Stop()
	}
	a.procs = nil
	return nil
}

// codexSession is a Codex Session implementation.
type codexSession struct {
	id string
	ch <-chan codingagent.StreamEvent
	pm *ProcessManager
}

// Send returns the streaming event channel.
// For single-shot sessions, the prompt is already sent via JSON-RPC at startup.
func (s *codexSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	return s.ch, nil
}

// ID returns the session identifier.
func (s *codexSession) ID() string { return s.id }

// Close terminates the session and stops the subprocess.
func (s *codexSession) Close() error { return s.pm.Stop() }
