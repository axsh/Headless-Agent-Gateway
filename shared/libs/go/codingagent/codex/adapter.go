package codex

import (
	"context"
	"fmt"
	"sync"

	"github.com/axsh/hag/codingagent"
)

// CodexAdapter is a CodingAgent implementation using the Codex CLI.
type CodexAdapter struct {
	config *codingagent.AdapterConfig
	mu     sync.Mutex
}

// compile-time interface compliance check
var _ codingagent.CodingAgent = (*CodexAdapter)(nil)

// New creates a CodexAdapter.
func New(config *codingagent.AdapterConfig) *CodexAdapter {
	return &CodexAdapter{config: config}
}

// Name returns "codex".
func (a *CodexAdapter) Name() string { return "codex" }

// CreateSession starts a new Codex session.
// Note: Full subprocess management is deferred to integration phase;
// this implementation provides the structural foundation.
func (a *CodexAdapter) CreateSession(
	ctx context.Context, opts ...codingagent.SessionOption,
) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)
	codingagent.ApplyDefaults(cfg, a.config)

	// Generate config.toml for Codex
	configPath, err := WriteConfigTOML(cfg.Model, a.config.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("codex: write config: %w", err)
	}

	_ = configPath // Will be used by process manager in full implementation
	return &codexSession{id: "codex-placeholder"}, nil
}

// Close releases resources.
func (a *CodexAdapter) Close() error {
	return nil
}

// codexSession is a Codex Session implementation.
type codexSession struct {
	id string
}

// Send returns the streaming event channel.
func (s *codexSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	ch := make(chan codingagent.StreamEvent)
	close(ch)
	return ch, nil
}

// ID returns the session identifier.
func (s *codexSession) ID() string { return s.id }

// Close terminates the session.
func (s *codexSession) Close() error { return nil }
