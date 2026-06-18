package config

import "github.com/axsh/arctic-tern/shared/libs/go/logger"

// AppConfig is the root configuration for tern.
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

	// TLS holds TLS configuration for the proxy server.
	TLS TLSConfig `yaml:"tls"`

	// AuthToken is the internal gateway authentication token.
	// Empty = auto-generate at startup. Non-empty = use as static token.
	AuthToken string `yaml:"auth_token"`

	// MaxRequestBodyBytes is the maximum request body size in bytes.
	// Default: 10MB (10485760).
	MaxRequestBodyBytes int64 `yaml:"max_request_body_bytes"`

	// Session holds session management settings.
	Session SessionConfig `yaml:"session"`

	// Server holds HTTP server timeout settings.
	Server ServerConfig `yaml:"server"`
}

// ApplyDefaults fills zero-valued fields with safe defaults.
func (c *LLMGatewayConfig) ApplyDefaults() {
	if c.MaxRequestBodyBytes == 0 {
		c.MaxRequestBodyBytes = 10 * 1024 * 1024 // 10MB
	}
	if c.Session.MaxSessions == 0 {
		c.Session.MaxSessions = 1000
	}
	if c.Session.TTLSeconds == 0 {
		c.Session.TTLSeconds = 86400 // 24h
	}
	if c.Server.ReadTimeoutSeconds == 0 {
		c.Server.ReadTimeoutSeconds = 30
	}
	if c.Server.WriteTimeoutSeconds == 0 {
		c.Server.WriteTimeoutSeconds = 300 // SSE streaming
	}
	if c.Server.IdleTimeoutSeconds == 0 {
		c.Server.IdleTimeoutSeconds = 60
	}
	if c.Server.MaxHeaderBytes == 0 {
		c.Server.MaxHeaderBytes = 1 << 20 // 1MB
	}
}

// TLSConfig holds TLS configuration for the proxy server.
type TLSConfig struct {
	// Enabled controls whether TLS is used. Default: false.
	Enabled bool `yaml:"enabled"`

	// Mode is the TLS mode: "auto" (self-signed), "file" (external cert).
	// Ignored when Enabled is false.
	Mode string `yaml:"mode"`

	// CertFile is the path to the TLS certificate (mode=file).
	CertFile string `yaml:"cert_file"`

	// KeyFile is the path to the TLS private key (mode=file).
	KeyFile string `yaml:"key_file"`

	// ExtraSANs is additional Subject Alternative Names (mode=auto).
	ExtraSANs []string `yaml:"extra_sans"`
}

// SessionConfig holds session management settings.
type SessionConfig struct {
	// MaxSessions is the maximum number of tracked sessions. Default: 1000.
	MaxSessions int `yaml:"max_sessions"`

	// TTLSeconds is the session TTL in seconds. Default: 86400 (24h).
	TTLSeconds int `yaml:"ttl_seconds"`
}

// ServerConfig holds HTTP server timeout settings.
type ServerConfig struct {
	// ReadTimeoutSeconds is the HTTP server read timeout. Default: 30.
	ReadTimeoutSeconds int `yaml:"read_timeout_seconds"`

	// WriteTimeoutSeconds is the HTTP server write timeout. Default: 300.
	WriteTimeoutSeconds int `yaml:"write_timeout_seconds"`

	// IdleTimeoutSeconds is the HTTP server idle timeout. Default: 60.
	IdleTimeoutSeconds int `yaml:"idle_timeout_seconds"`

	// MaxHeaderBytes is the maximum header size. Default: 1MB.
	MaxHeaderBytes int `yaml:"max_header_bytes"`
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

	// Outputs defines log output destinations.
	// If empty, defaults to stdout.
	Outputs []logger.LogOutputConfig `yaml:"outputs,omitempty"`
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
	// DisableSandbox disables the CLI internal sandbox for coding agents.
	// Useful for container/CI environments where the sandbox path mapping
	// causes files to be created in different locations.
	DisableSandbox bool `yaml:"disable_sandbox"`
}
