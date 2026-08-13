package mcp

import (
	"context"
	"log/slog"
	"sync"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

// Manager owns session-scoped MCP server connections.
type Manager struct {
	sessionID string
	vault     SecretResolver
	logger    *slog.Logger
	dial      DialFunc

	mu      sync.Mutex
	clients map[string]ServerClient
	failed  map[string]string
}

// NewManager creates a Manager. dial may be nil to use DialMCP.
func NewManager(sessionID string, vault SecretResolver, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		sessionID: sessionID,
		vault:     vault,
		logger:    logger,
		dial:      DialMCP,
		clients:   make(map[string]ServerClient),
		failed:    make(map[string]string),
	}
}

// WithDial replaces the dial function (tests).
func (m *Manager) WithDial(d DialFunc) *Manager {
	m.dial = d
	return m
}

// ConnectAll connects enabled servers. Per-server failures are recorded; does not hard-fail.
func (m *Manager) ConnectAll(ctx context.Context, servers map[string]toolconfig.MCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, cfg := range servers {
		if cfg.Enabled != nil && !*cfg.Enabled {
			m.logger.Debug("mcp server disabled", "session_id", m.sessionID, "mcp_server", name)
			continue
		}
		resolved, err := ResolveServerSecrets(cfg, m.vault)
		if err != nil {
			m.failed[name] = err.Error()
			m.logger.Warn("mcp secret resolve failed",
				"session_id", m.sessionID, "mcp_server", name, "transport", cfg.Transport, "err", err)
			continue
		}
		client, err := m.dial(ctx, name, resolved)
		if err != nil {
			m.failed[name] = err.Error()
			m.logger.Warn("mcp connect failed",
				"session_id", m.sessionID, "mcp_server", name, "transport", cfg.Transport, "err", err)
			continue
		}
		m.clients[name] = client
		m.logger.Info("mcp connected",
			"session_id", m.sessionID, "mcp_server", name, "transport", cfg.Transport)
	}
	return nil
}

// Failed returns a copy of per-server connection errors.
func (m *Manager) Failed() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.failed))
	for k, v := range m.failed {
		out[k] = v
	}
	return out
}

// ListAllTools returns tools grouped by server name.
func (m *Manager) ListAllTools(ctx context.Context) (map[string][]ToolInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]ToolInfo, len(m.clients))
	for name, c := range m.clients {
		tools, err := c.ListTools(ctx)
		if err != nil {
			m.logger.Warn("mcp list tools failed",
				"session_id", m.sessionID, "mcp_server", name, "err", err)
			m.failed[name] = err.Error()
			continue
		}
		out[name] = tools
	}
	return out, nil
}

// Call invokes a tool on a named server.
func (m *Manager) Call(ctx context.Context, server, tool string, args map[string]any) (string, error) {
	m.mu.Lock()
	c, ok := m.clients[server]
	m.mu.Unlock()
	if !ok {
		return "", errServerUnavailable(server)
	}
	m.logger.Debug("mcp call tool",
		"session_id", m.sessionID, "mcp_server", server, "tool", tool)
	return c.CallTool(ctx, tool, args)
}

// Close closes all clients.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var first error
	for name, c := range m.clients {
		if err := c.Close(); err != nil && first == nil {
			first = err
			m.logger.Warn("mcp close failed",
				"session_id", m.sessionID, "mcp_server", name, "err", err)
		}
		delete(m.clients, name)
	}
	m.logger.Debug("mcp manager closed", "session_id", m.sessionID)
	return first
}

type unavailableError struct{ server string }

func errServerUnavailable(server string) error {
	return &unavailableError{server: server}
}

func (e *unavailableError) Error() string {
	return "mcp server unavailable: " + e.server
}
