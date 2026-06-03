package llmgateway

import (
	"context"
	"fmt"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/vault"
)

// BifrostDriver implements LLMGatewayBackend using Bifrost SDK.
// It wraps a ProxyServer for HTTP routing and uses Bifrost SDK for LLM provider communication.
type BifrostDriver struct {
	cfg      *config.AppConfig
	profiles *config.ModelProfilesConfig
	vault    vault.VaultStore
	logger   logger.Logger
	proxy    *ProxyServer    // HTTP frontend
	router   *ModelRouter    // model routing
	account  *BifrostAccount // Bifrost SDK account adapter
}

// NewBifrostDriver creates a BifrostDriver.
// cfg is required; profiles, vs, and log may be nil.
func NewBifrostDriver(
	cfg *config.AppConfig,
	profiles *config.ModelProfilesConfig,
	vs vault.VaultStore,
	log logger.Logger,
) (*BifrostDriver, error) {
	if cfg == nil {
		cfg = &config.AppConfig{}
	}
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}

	d := &BifrostDriver{
		cfg:      cfg,
		profiles: profiles,
		vault:    vs,
		logger:   log.WithComponent("bifrost-driver"),
		router:   NewModelRouter(profiles, log),
		account:  NewBifrostAccount(profiles, vs, log),
	}

	// Create the ProxyServer as the HTTP frontend.
	// We pass an empty profiles path so ProxyServer doesn't try to load profiles from file.
	proxyCfg := *cfg
	proxyCfg.LLMGateway.ModelProfilesPath = ""
	proxy, err := NewProxyServer(&proxyCfg, vs, log)
	if err != nil {
		return nil, fmt.Errorf("bifrost driver: create proxy: %w", err)
	}
	proxy.profiles = profiles
	proxy.driver = d
	d.proxy = proxy

	return d, nil
}

// Launch starts the HTTP proxy server.
func (d *BifrostDriver) Launch(ctx context.Context) error {
	d.logger.Info("launching bifrost driver")
	return d.proxy.Launch(ctx)
}

// Shutdown gracefully stops the HTTP proxy server.
func (d *BifrostDriver) Shutdown(ctx context.Context) error {
	d.logger.Info("shutting down bifrost driver")
	return d.proxy.Shutdown(ctx)
}

// ListModels returns the list of configured models from profiles.
func (d *BifrostDriver) ListModels() []ModelInfo {
	return d.proxy.ListModels()
}

// Health returns the backend health status.
func (d *BifrostDriver) Health() HealthStatus {
	return d.proxy.Health()
}

// ProxyURL returns the HTTP proxy URL.
func (d *BifrostDriver) ProxyURL() string {
	return d.proxy.ProxyURL()
}

// Compile-time check that BifrostDriver implements LLMGatewayBackend.
var _ LLMGatewayBackend = (*BifrostDriver)(nil)
