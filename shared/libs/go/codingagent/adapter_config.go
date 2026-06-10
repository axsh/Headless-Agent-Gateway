package codingagent

import "github.com/axsh/hag/logger"

// AdapterConfig is the common configuration for all coding agent adapters.
type AdapterConfig struct {
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
}
