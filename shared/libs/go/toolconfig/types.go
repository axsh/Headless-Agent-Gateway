package toolconfig

import "encoding/json"

// MCPServerConfig is one MCP server entry (Client API / SessionRecord).
type MCPServerConfig struct {
	Transport string            `json:"transport"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

// FunctionConfig is a client-defined function schema (no handler body).
type FunctionConfig struct {
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// MCPToolNamePrefix is reserved for Wayfinder-registered MCP tools.
const MCPToolNamePrefix = "mcp__"
