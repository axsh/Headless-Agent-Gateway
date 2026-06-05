package codingagent

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
	SDKSessionID string // CLI/SDK-managed session ID for context resume

	// VFS mounts (container execution)
	VFSMounts []VFSMount // Host->container file mappings
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

// WithSDKSessionID sets the SDK session ID for context resume.
func WithSDKSessionID(id string) SessionOption {
	return func(c *SessionConfig) { c.SDKSessionID = id }
}

// WithVFSMounts sets the VFS mount mappings.
func WithVFSMounts(mounts []VFSMount) SessionOption {
	return func(c *SessionConfig) { c.VFSMounts = mounts }
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
}
