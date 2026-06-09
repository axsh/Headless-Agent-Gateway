package claudecode

import (
	"context"
	"fmt"
	"sync"

	"github.com/axsh/hag/codingagent"
)

// ClaudeCodeAdapter is a CodingAgent implementation using the Claude Code CLI.
type ClaudeCodeAdapter struct {
	config *codingagent.AdapterConfig
	mu     sync.Mutex
	procs  []*ProcessManager
}

// compile-time interface compliance check
var _ codingagent.CodingAgent = (*ClaudeCodeAdapter)(nil)

// New creates a ClaudeCodeAdapter.
func New(config *codingagent.AdapterConfig) *ClaudeCodeAdapter {
	return &ClaudeCodeAdapter{config: config}
}

// Name returns "claudecode".
func (a *ClaudeCodeAdapter) Name() string { return "claudecode" }

// CreateSession starts a new Claude Code session by launching the CLI subprocess.
func (a *ClaudeCodeAdapter) CreateSession(
	ctx context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	codingagent.ApplyDefaults(cfg, a.config)

	ch, pm, err := StartProcess(ctx, a.config, cfg)
	if err != nil {
		return nil, fmt.Errorf("claudecode: create session: %w", err)
	}

	a.mu.Lock()
	a.procs = append(a.procs, pm)
	a.mu.Unlock()

	sid := fmt.Sprintf("claude-%d", pm.cmd.Process.Pid)
	return &claudeSession{id: sid, ch: ch, pm: pm}, nil
}

// Close stops all active processes and releases resources.
func (a *ClaudeCodeAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, pm := range a.procs {
		pm.Stop()
	}
	a.procs = nil
	return nil
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
