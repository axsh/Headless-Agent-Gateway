package config

// AppConfig is the root configuration for HAG.
type AppConfig struct {
	// LLMGateway holds LLM Gateway Proxy settings.
	LLMGateway LLMGatewayConfig `yaml:"llm_gateway"`

	// Vault holds VaultStore settings.
	Vault VaultConfig `yaml:"vault"`

	// Log holds logging settings.
	Log LogConfig `yaml:"log"`

	// WebSocket holds WebSocket server settings.
	WebSocket WebSocketConfig `yaml:"websocket"`

	// AgentService holds AgentService HTTP settings.
	AgentService AgentServiceConfig `yaml:"agent_service"`
}

// LLMGatewayConfig holds LLM Gateway Proxy settings.
type LLMGatewayConfig struct {
	// Port is the HTTP proxy listen port.
	Port int `yaml:"port"`

	// ModelProfilesPath is the path to model_profiles.yaml.
	ModelProfilesPath string `yaml:"model_profiles_path"`

	// MetricsEnabled controls Bifrost metrics collection.
	MetricsEnabled bool `yaml:"metrics_enabled"`

	// Retry holds retry configuration for upstream provider requests.
	Retry RetrySettings `yaml:"retry"`
}

// RetrySettings holds retry configuration for upstream provider requests.
type RetrySettings struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retry).
	MaxRetries int `yaml:"max_retries"`

	// InitialDelaySeconds is the base delay in seconds for exponential backoff.
	InitialDelaySeconds int `yaml:"initial_delay_seconds"`

	// MaxDelaySeconds is the maximum delay in seconds between retries.
	MaxDelaySeconds int `yaml:"max_delay_seconds"`
}

// VaultConfig holds VaultStore settings.
type VaultConfig struct {
	// Backend is the VaultStore backend type: "env", "file", "keyring".
	Backend string `yaml:"backend"`

	// FilePath is the file path for FileVaultBackend.
	FilePath string `yaml:"file_path,omitempty"`

	// AESEnabled enables AES encryption for FileVaultBackend.
	AESEnabled bool `yaml:"aes_enabled,omitempty"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	// Level is the minimum log level: "debug", "info", "warn", "error".
	Level string `yaml:"level"`
}

// WebSocketConfig holds WebSocket server settings.
type WebSocketConfig struct {
	// Port is the WebSocket server listen port.
	// When 0, the OS assigns an ephemeral port.
	Port int `yaml:"port"`
}

// AgentServiceConfig holds AgentService HTTP settings.
type AgentServiceConfig struct {
	// Port is the AgentService HTTP listen port.
	// When 0, the OS assigns an ephemeral port.
	Port int `yaml:"port"`
}
