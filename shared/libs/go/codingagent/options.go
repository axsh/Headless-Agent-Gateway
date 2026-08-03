package codingagent

import "path/filepath"

// SessionOption configures a session at creation time.
type SessionOption func(*SessionConfig)

// SessionConfig holds session creation parameters.
type SessionConfig struct {
	// Request-level options (vary per request)
	Model        string   // Model name (e.g., "anthropic/claude-sonnet-4")
	Prompt       string   // Initial prompt (for single-shot sessions)
	AllowedTools []string // Allowed tool names

	// Environment-level options (fixed at process/container start)
	WorkDir string            // Working directory (CWD)
	EnvVars map[string]string // Additional environment variables

	// Session resume
	AgentSessionID string // Agent-managed session ID for context resume

	// Session data storage directory.
	// When set, the adapter maps this to the agent-specific env var
	// (e.g., CLAUDE_CONFIG_DIR, CODEX_HOME).
	// Falls back to WorkDir if not explicitly set.
	SessionDir string

	// VFS mounts (container execution)
	VFSMounts []VFSMount // Host->container file mappings

	// MaxTurns limits the number of agent turns. 0 means CLI default.
	MaxTurns int

	// ExecutionMode controls stdin behavior: "interactive" or "single_shot".
	ExecutionMode string
	// IdleTimeoutSeconds is the max idle time without stdout/stderr output.
	IdleTimeoutSeconds int
	// MaxExecutionSeconds is the max wall-clock execution time.
	MaxExecutionSeconds int
	// ScannerMaxTokenBytes is the max JSONL line size for agent stdout scanners.
	ScannerMaxTokenBytes int
	// MaxToolResultBytes is the max EventToolResult content size for SSE relay.
	MaxToolResultBytes int
}

// WithModel sets the model name.
func WithModel(model string) SessionOption {
	return func(c *SessionConfig) { c.Model = model }
}

// WithPrompt sets the initial prompt.
func WithPrompt(prompt string) SessionOption {
	return func(c *SessionConfig) { c.Prompt = prompt }
}

// WithAllowedTools sets the allowed tool names.
func WithAllowedTools(tools []string) SessionOption {
	return func(c *SessionConfig) { c.AllowedTools = tools }
}

// WithWorkDir sets the working directory.
func WithWorkDir(dir string) SessionOption {
	return func(c *SessionConfig) { c.WorkDir = dir }
}

// WithEnvVars sets additional environment variables.
func WithEnvVars(vars map[string]string) SessionOption {
	return func(c *SessionConfig) { c.EnvVars = vars }
}

// WithAgentSessionID sets the agent session ID for context resume.
func WithAgentSessionID(id string) SessionOption {
	return func(c *SessionConfig) { c.AgentSessionID = id }
}

// WithVFSMounts sets the VFS mount mappings.
func WithVFSMounts(mounts []VFSMount) SessionOption {
	return func(c *SessionConfig) { c.VFSMounts = mounts }
}

// WithMaxTurns sets the maximum number of agent turns.
func WithMaxTurns(n int) SessionOption {
	return func(c *SessionConfig) { c.MaxTurns = n }
}

// WithSessionDir sets the session data storage directory.
func WithSessionDir(dir string) SessionOption {
	return func(c *SessionConfig) { c.SessionDir = dir }
}

// WithExecutionMode sets the execution mode for stdin handling.
func WithExecutionMode(mode string) SessionOption {
	return func(c *SessionConfig) { c.ExecutionMode = NormalizeExecutionMode(mode) }
}

// WithIdleTimeout sets the idle timeout in seconds.
func WithIdleTimeout(seconds int) SessionOption {
	return func(c *SessionConfig) { c.IdleTimeoutSeconds = seconds }
}

// WithMaxExecution sets the max execution time in seconds.
func WithMaxExecution(seconds int) SessionOption {
	return func(c *SessionConfig) { c.MaxExecutionSeconds = seconds }
}

// WithScannerMaxTokenBytes sets the max JSONL line size for stdout scanners.
func WithScannerMaxTokenBytes(n int) SessionOption {
	return func(c *SessionConfig) { c.ScannerMaxTokenBytes = n }
}

// WithMaxToolResultBytes sets the max EventToolResult content size for SSE relay.
func WithMaxToolResultBytes(n int) SessionOption {
	return func(c *SessionConfig) { c.MaxToolResultBytes = n }
}

// NewSessionConfig applies the given SessionOptions and returns a SessionConfig.
func NewSessionConfig(opts ...SessionOption) *SessionConfig {
	cfg := &SessionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// ApplyDefaults applies AdapterConfig default values to SessionConfig.
// Fields explicitly set by SessionOption are not overwritten.
// Priority: SessionOption > AdapterConfig.Default* > zero value
func ApplyDefaults(cfg *SessionConfig, ac *AdapterConfig) {
	if cfg.WorkDir == "" {
		cfg.WorkDir = ac.DefaultWorkDir
	}
	// R2: Resolve WorkDir to absolute path.
	// Relative paths cause issues when used as base for SessionDir
	// or as cmd.Dir for subprocess execution.
	if cfg.WorkDir != "" {
		if abs, err := filepath.Abs(cfg.WorkDir); err == nil {
			cfg.WorkDir = abs
		}
	}
	if cfg.Model == "" {
		cfg.Model = ac.DefaultModel
	}
	if cfg.EnvVars == nil && ac.DefaultEnvVars != nil {
		cfg.EnvVars = make(map[string]string)
		for k, v := range ac.DefaultEnvVars {
			cfg.EnvVars[k] = v
		}
	}
	// SessionDir fallback: explicit > AdapterConfig > WorkDir/.AgentName > WorkDir
	if cfg.SessionDir == "" {
		if ac.DefaultSessionDir != "" {
			cfg.SessionDir = ac.DefaultSessionDir
		} else if cfg.WorkDir != "" && ac.AgentName != "" {
			cfg.SessionDir = filepath.Join(cfg.WorkDir, "."+ac.AgentName)
		} else if cfg.WorkDir != "" {
			cfg.SessionDir = cfg.WorkDir
		}
	}
	// R1: Resolve SessionDir to absolute path.
	// CLI tools (claude, codex) resolve CLAUDE_CONFIG_DIR / CODEX_HOME
	// relative to their CWD, not the caller's CWD, causing path duplication.
	if cfg.SessionDir != "" {
		if abs, err := filepath.Abs(cfg.SessionDir); err == nil {
			cfg.SessionDir = abs
		}
	}
	if cfg.ExecutionMode == "" {
		cfg.ExecutionMode = NormalizeExecutionMode(ac.ExecutionMode)
	}
	if cfg.IdleTimeoutSeconds == 0 {
		if ac.IdleTimeoutSeconds > 0 {
			cfg.IdleTimeoutSeconds = ac.IdleTimeoutSeconds
		} else {
			cfg.IdleTimeoutSeconds = 300
		}
	}
	if cfg.MaxExecutionSeconds == 0 {
		if ac.MaxExecutionSeconds > 0 {
			cfg.MaxExecutionSeconds = ac.MaxExecutionSeconds
		} else {
			cfg.MaxExecutionSeconds = 3600
		}
	}
	if cfg.ScannerMaxTokenBytes == 0 && ac.ScannerMaxTokenBytes > 0 {
		cfg.ScannerMaxTokenBytes = ac.ScannerMaxTokenBytes
	}
	if cfg.MaxToolResultBytes == 0 && ac.MaxToolResultBytes > 0 {
		cfg.MaxToolResultBytes = ac.MaxToolResultBytes
	}
}
