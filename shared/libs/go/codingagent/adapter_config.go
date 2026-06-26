package codingagent

import "github.com/axsh/arctic-tern/shared/libs/go/logger"

// AdapterConfig is the common configuration for all coding agent adapters.
type AdapterConfig struct {
	// AgentName is the agent name used for directory naming (e.g., "claudecode", "codex").
	AgentName string

	// GatewayURL is the LLM Gateway Proxy URL.
	GatewayURL string

	// Logger is the logger instance.
	Logger logger.Logger

	// DefaultWorkDir is the default working directory (CWD).
	// Can be overridden per-session via WithWorkDir.
	DefaultWorkDir string

	// DefaultModel is the default model name.
	// Can be overridden per-session via WithModel.
	DefaultModel string

	// DefaultEnvVars is the default additional environment variables.
	// Can be overridden per-session via WithEnvVars.
	DefaultEnvVars map[string]string

	// DefaultSessionDir is the default session data storage directory.
	// Can be overridden per-session via WithSessionDir.
	// Falls back to WorkDir if not set.
	DefaultSessionDir string

	// DisableSandbox disables the CLI internal sandbox (for container execution).
	// When true, CLAUDE_CODE_SKIP_SANDBOX=1 is set.
	DisableSandbox bool

	// ToolCallFallback enables text-to-tool-call conversion in the Gateway.
	// When true, the ANTHROPIC_API_KEY includes ";fallback=true" metadata
	// so the gateway proxy can apply fallback logic for models that
	// sometimes emit tool calls as text instead of proper function_call.
	ToolCallFallback bool

	// ModelMode is the wire API mode for the adapter ("chat" or "responses").
	// Used by Codex to determine config.toml wire_api value.
	// Empty string defaults to "chat".
	ModelMode string

	// GatewayToken is the internal authentication token for LLMGP.
	// Injected by server.Server on startup.
	GatewayToken string

	// EnableSubagent enables subagent delegation for WBS node execution.
	// When true, each WBS node runs in an independent child session.
	EnableSubagent bool
}
