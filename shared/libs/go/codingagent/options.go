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
}
