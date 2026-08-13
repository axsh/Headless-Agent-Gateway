package claudecode

import (
	"context"
	"fmt"
	"sync"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// ClaudeCodeAdapter is a CodingAgent implementation using the Claude Code CLI.
type ClaudeCodeAdapter struct {
	config *codingagent.AdapterConfig
	logger logger.Logger
	mu     sync.Mutex
	procs  []*ProcessManager
}

// compile-time interface compliance check
var _ codingagent.CodingAgent = (*ClaudeCodeAdapter)(nil)

// New creates a ClaudeCodeAdapter.
func New(config *codingagent.AdapterConfig) *ClaudeCodeAdapter {
	log := config.Logger
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	return &ClaudeCodeAdapter{
		config: config,
		logger: log.WithComponent("claudecode"),
	}
}

// Name returns "claudecode".
func (a *ClaudeCodeAdapter) Name() string { return "claudecode" }

// CreateSession starts a new Claude Code session by launching the CLI subprocess.
func (a *ClaudeCodeAdapter) CreateSession(
	ctx context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	codingagent.ApplyDefaults(cfg, a.config)

	a.logger.Debug("creating claude code session",
		"model", cfg.Model, "work_dir", cfg.WorkDir,
		"session_dir", cfg.SessionDir, "config_dir", cfg.ConfigDir)

	if err := ApplyClaudeConfigDir(cfg.SessionDir, cfg.ConfigDir); err != nil {
		return nil, fmt.Errorf("claudecode: apply config_dir: %w", err)
	}

	if len(cfg.MCPServers) > 0 {
		keys := ManagedMCPKeys(cfg.MCPServers)
		if err := InjectMCPServers(cfg.WorkDir, keys, cfg.MCPServers, nil); err != nil {
			return nil, fmt.Errorf("claudecode: inject mcp: %w", err)
		}
		a.logger.Info("injected mcp servers into .mcp.json",
			"work_dir", cfg.WorkDir, "count", len(cfg.MCPServers))
	}

	ch, pm, err := StartProcess(ctx, a.config, cfg)
	if err != nil {
		return nil, fmt.Errorf("claudecode: create session: %w", err)
	}

	a.mu.Lock()
	a.procs = append(a.procs, pm)
	a.mu.Unlock()

	sid := fmt.Sprintf("claude-%d", pm.cmd.Process.Pid)
	a.logger.Debug("claude code session created", "session_id", sid)
	return &claudeSession{id: sid, ch: ch, pm: pm}, nil
}

// Close stops all active processes and releases resources.
func (a *ClaudeCodeAdapter) Close() error {
	a.logger.Debug("closing claude code adapter")
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, pm := range a.procs {
		pm.Stop()
	}
	a.procs = nil
	return nil
}

// SetGatewayToken sets the gateway token in the adapter config.
func (a *ClaudeCodeAdapter) SetGatewayToken(token string) {
	a.config.GatewayToken = token
}

// claudeSession is a Claude Code Session implementation.
type claudeSession struct {
	id string
	ch <-chan codingagent.StreamEvent
	pm *ProcessManager
}

// Send returns the streaming event channel.
// For single-shot sessions, the prompt is already set at CreateSession time.
func (s *claudeSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	return s.ch, nil
}

// ID returns the session identifier.
func (s *claudeSession) ID() string { return s.id }

// Close terminates the session and stops the subprocess.
func (s *claudeSession) Close() error { return s.pm.Stop() }

// WriteStdin writes additional input to the running Claude process.
func (s *claudeSession) WriteStdin(text string) error { return s.pm.WriteStdin(text) }

// SupportsMultimodal returns true: Claude Code CLI supports image inputs.
func (a *ClaudeCodeAdapter) SupportsMultimodal() bool {
	return true
}
