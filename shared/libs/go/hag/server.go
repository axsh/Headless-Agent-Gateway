// Package hag provides the HAG (Headless-Agent-Gateway) core facade.
// Users interact with HAG through the Server type, which orchestrates
// all components (LLM Gateway, Config, Vault, Logger).
package hag

import (
	"context"
	"fmt"

	"github.com/axsh/hag/agentservice"
	"github.com/axsh/hag/config"
	"github.com/axsh/hag/llmgateway"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/vault"
	"github.com/axsh/hag/wsserver"
)

// Server is the HAG core facade that orchestrates all components.
// Users interact with HAG through this type.
type Server struct {
	cfg          *config.AppConfig
	logger       logger.Logger
	vault        vault.VaultStore
	gateway      llmgateway.LLMGatewayBackend
	agentService *agentservice.Server
	wsServer     *wsserver.Server
}

// New creates a new HAG Server with the given options.
// No goroutines are started; no network listeners are opened.
//
// Initialization order (per 000-Architecture R3-3):
//  1. Apply all Options
//  2. Resolve Config (WithConfigPath -> Load, WithConfig -> use, none -> default)
//  3. Resolve Logger (WithLogger or default from Config.Log)
//  4. Resolve VaultStore (WithVaultStore or default from Config.Vault)
//  5. Resolve Gateway (WithGateway or NewProxyServer from Config)
func New(opts ...Option) (*Server, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	// Step 2: Resolve Config.
	cfg, err := resolveConfig(o)
	if err != nil {
		return nil, fmt.Errorf("hag: %w", err)
	}

	// Step 3: Resolve Logger.
	log := resolveLogger(o, cfg)

	// Step 4: Resolve VaultStore.
	vs := resolveVault(o)

	// Step 5: Resolve Gateway.
	gw, err := resolveGateway(o, cfg, vs, log)
	if err != nil {
		return nil, fmt.Errorf("hag: %w", err)
	}

	as := agentservice.New()
	ws := wsserver.New()

	return &Server{
		cfg:          cfg,
		logger:       log,
		vault:        vs,
		gateway:      gw,
		agentService: as,
		wsServer:     ws,
	}, nil
}

// Launch starts all components. Non-blocking.
// Currently starts the LLM Gateway Proxy.
func (s *Server) Launch(ctx context.Context) error {
	s.logger.Info("starting HAG server")

	if err := s.gateway.Launch(ctx); err != nil {
		return fmt.Errorf("hag: gateway launch: %w", err)
	}

	if err := s.wsServer.Launch(ctx); err != nil {
		return fmt.Errorf("hag: wsserver launch: %w", err)
	}

	s.logger.Info("HAG server started")
	return nil
}

// Shutdown gracefully stops all components in reverse launch order.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HAG server")

	if err := s.wsServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("hag: wsserver shutdown: %w", err)
	}

	if err := s.gateway.Shutdown(ctx); err != nil {
		return fmt.Errorf("hag: gateway shutdown: %w", err)
	}

	s.logger.Info("HAG server stopped")
	return nil
}

// Gateway returns the LLM Gateway Proxy backend.
func (s *Server) Gateway() llmgateway.LLMGatewayBackend {
	return s.gateway
}

// AgentService returns the AgentService instance.
func (s *Server) AgentService() agentservice.AgentService {
	return s.agentService
}

// ReloadModelProfiles reloads the model profiles at runtime.
func (s *Server) ReloadModelProfiles(path string) error {
	profiles, err := config.LoadModelProfiles(path)
	if err != nil {
		return err
	}
	if bd, ok := s.gateway.(*llmgateway.BifrostDriver); ok {
		bd.ReloadProfiles(profiles)
	} else if ps, ok := s.gateway.(*llmgateway.ProxyServer); ok {
		ps.ReloadProfiles(profiles)
	}
	return nil
}

// resolveConfig resolves the AppConfig from options.
// Priority: WithConfigPath > WithConfig > default.
func resolveConfig(o *options) (*config.AppConfig, error) {
	if o.configPath != "" {
		cfg, err := config.Load(o.configPath)
		if err != nil {
			return nil, fmt.Errorf("load config from %s: %w", o.configPath, err)
		}
		return cfg, nil
	}
	if o.cfg != nil {
		return o.cfg, nil
	}
	return &config.AppConfig{}, nil
}

// resolveLogger resolves the Logger from options.
// If WithLogger is set, use it. Otherwise create a default from Config.Log.
func resolveLogger(o *options, cfg *config.AppConfig) logger.Logger {
	if o.logger != nil {
		return o.logger
	}
	level := logger.ParseLevel(cfg.Log.Level)
	return logger.NewDefault(level)
}

// resolveVault resolves the VaultStore from options.
// If WithVaultStore is set, use it. Otherwise create an EnvVaultBackend.
func resolveVault(o *options) vault.VaultStore {
	if o.vault != nil {
		return o.vault
	}
	return vault.NewEnvVaultBackend()
}

// resolveGateway resolves the LLMGatewayBackend from options.
// If WithGateway is set, use it. Otherwise:
//   - If model profiles are configured, create a BifrostDriver.
//   - Otherwise create a standalone ProxyServer.
func resolveGateway(o *options, cfg *config.AppConfig, vs vault.VaultStore, log logger.Logger) (llmgateway.LLMGatewayBackend, error) {
	if o.gateway != nil {
		return o.gateway, nil
	}

	// If model profiles path is configured, try to load and use BifrostDriver.
	if cfg.LLMGateway.ModelProfilesPath != "" {
		profiles, err := config.LoadModelProfiles(cfg.LLMGateway.ModelProfilesPath)
		if err != nil {
			return nil, fmt.Errorf("load model profiles: %w", err)
		}
		return llmgateway.NewBifrostDriver(cfg, profiles, vs, log)
	}

	// No profiles configured; use standalone ProxyServer.
	return llmgateway.NewProxyServer(cfg, vs, log)
}
