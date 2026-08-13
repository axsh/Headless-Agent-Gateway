package mcp

import (
	"context"
)

// ToolInfo describes a tool exposed by an MCP server.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ServerClient is one connected MCP server.
type ServerClient interface {
	ListTools(ctx context.Context) ([]ToolInfo, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
	Close() error
}

// SecretResolver resolves vault:// references (optional).
type SecretResolver interface {
	Resolve(ref string) (string, error)
}

// DialFunc opens a transport for one server config.
type DialFunc func(ctx context.Context, name string, cfg ResolvedServer) (ServerClient, error)
