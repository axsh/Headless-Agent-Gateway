package hag

import (
	"github.com/axsh/hag/config"
	"github.com/axsh/hag/llmgateway"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/vault"
)

// Option configures a Server.
type Option func(*options)

type options struct {
	cfg        *config.AppConfig
	configPath string
	logger     logger.Logger
	vault      vault.VaultStore
	gateway    llmgateway.LLMGatewayBackend
}

// WithConfig sets the configuration directly.
// config.Load() can be used to generate the struct, which is then passed here.
func WithConfig(cfg *config.AppConfig) Option {
	return func(o *options) {
		o.cfg = cfg
	}
}

// WithConfigPath sets the configuration file path.
// Internally calls config.Load(path) during New().
func WithConfigPath(path string) Option {
	return func(o *options) {
		o.configPath = path
	}
}

// WithLogger injects a custom Logger implementation.
// When nil, a default logger is created based on Config.Log settings.
func WithLogger(log logger.Logger) Option {
	return func(o *options) {
		o.logger = log
	}
}

// WithVaultStore injects a custom VaultStore implementation.
// When nil, a default backend is created based on Config.Vault settings.
func WithVaultStore(vs vault.VaultStore) Option {
	return func(o *options) {
		o.vault = vs
	}
}

// WithGateway injects a custom LLMGatewayBackend implementation.
// Useful for testing or custom backend implementations.
func WithGateway(gw llmgateway.LLMGatewayBackend) Option {
	return func(o *options) {
		o.gateway = gw
	}
}
