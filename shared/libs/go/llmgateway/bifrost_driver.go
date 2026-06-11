package llmgateway

import (
	"context"
	"fmt"

	bifrost "github.com/maximhq/bifrost/core"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/config"
	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/vault"
)

// BifrostDriver implements LLMGatewayBackend using Bifrost SDK.
// It wraps a ProxyServer for HTTP routing and uses Bifrost SDK for LLM provider communication.
type BifrostDriver struct {
	cfg        *config.AppConfig
	profiles   *config.ModelProfilesConfig
	vault      vault.VaultStore
	logger     logger.Logger
	proxy      *ProxyServer    // HTTP frontend
	router     *ModelRouter    // model routing
	account    *BifrostAccount // Bifrost SDK account adapter
	bifrostSDK *bifrost.Bifrost // Bifrost SDK instance for provider-specific request conversion
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
		router:   NewModelRouter(profiles, cfg, log),
		account:  NewBifrostAccount(profiles, vs, log),
	}

	// Initialize Bifrost SDK instance for provider-specific request conversion.
	bi, err := initBifrostSDK(d.account, log)
	if err != nil {
		// Log warning but continue — legacy forwarder path will be used as fallback.
		d.logger.Warn("bifrost SDK init failed, using legacy forwarder", "error", err)
	} else {
		d.bifrostSDK = bi
		d.logger.Debug("bifrost SDK initialized")
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

// initBifrostSDK creates a new Bifrost SDK instance from the given account.
func initBifrostSDK(account *BifrostAccount, log logger.Logger) (*bifrost.Bifrost, error) {
	bifrostCfg := bifrostSchemas.BifrostConfig{
		Account:         account,
		Logger:          nil, // use Bifrost default logger
		InitialPoolSize: 10,  // small pool — tern has low concurrency
	}
	bi, err := bifrost.Init(context.Background(), bifrostCfg)
	if err != nil {
		return nil, fmt.Errorf("bifrost SDK init: %w", err)
	}
	return bi, nil
}

// Launch starts the HTTP proxy server.
func (d *BifrostDriver) Launch(ctx context.Context) error {
	d.logger.Info("launching bifrost driver")
	return d.proxy.Launch(ctx)
}

// Shutdown gracefully stops the HTTP proxy server and Bifrost SDK.
func (d *BifrostDriver) Shutdown(ctx context.Context) error {
	d.logger.Info("shutting down bifrost driver")
	if d.bifrostSDK != nil {
		d.bifrostSDK.Shutdown()
		d.logger.Debug("bifrost SDK shut down")
	}
	return d.proxy.Shutdown(ctx)
}

// ReloadProfiles updates the loaded model profiles at runtime.
func (d *BifrostDriver) ReloadProfiles(profiles *config.ModelProfilesConfig) {
	d.profiles = profiles
	d.router = NewModelRouter(profiles, d.cfg, d.logger)
	d.account = NewBifrostAccount(profiles, d.vault, d.logger)

	// Reinitialize Bifrost SDK with new account.
	if d.bifrostSDK != nil {
		d.bifrostSDK.Shutdown()
	}
	bi, err := initBifrostSDK(d.account, d.logger)
	if err != nil {
		d.logger.Error("failed to reinit bifrost SDK after profile reload", "error", err)
	} else {
		d.bifrostSDK = bi
		d.logger.Debug("bifrost SDK reinitialized after profile reload")
	}

	if d.proxy != nil {
		d.proxy.ReloadProfiles(profiles)
	}
}

// ListModels returns the list of configured models from profiles.
func (d *BifrostDriver) ListModels() []ModelInfo {
	return d.proxy.ListModels()
}

// DefaultModel returns the default model from profiles.
func (d *BifrostDriver) DefaultModel() *ModelInfo {
	return d.proxy.DefaultModel()
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
