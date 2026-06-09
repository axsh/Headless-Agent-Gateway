package codingagent

import "context"

// CodingAgent is the common interface for coding agent backends.
// Both CLI wrapper types (Claude Code, Codex) and future direct API types
// can implement this interface.
type CodingAgent interface {
	// CreateSession starts a new agent session.
	// Internally launches a CLI subprocess.
	CreateSession(ctx context.Context, opts ...SessionOption) (Session, error)

	// Name returns the agent backend name ("claudecode", "codex").
	Name() string

	// SupportedProviders returns the list of LLM provider names this agent supports.
	// Returns nil or empty to indicate no provider restriction (all providers accepted).
	SupportedProviders() []string

	// Close releases agent resources.
	Close() error
}

// Session is an active agent session.
// Corresponds to the lifecycle of a CLI subprocess.
type Session interface {
	// Send sends a message and returns a streaming event channel.
	// The channel is closed when the agent response completes.
	Send(ctx context.Context, message string) (<-chan StreamEvent, error)

	// ID returns the session ID.
	ID() string

	// Close terminates the session and cleans up the subprocess.
	Close() error
}
