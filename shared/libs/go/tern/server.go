// Package tern provides the tern (arctic-tern) core facade.
// Users interact with tern through the Server type, which orchestrates
// all components (LLM Gateway, Config, Vault, Logger).
package tern

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/axsh/arctic-tern/agentservice"
	"github.com/axsh/arctic-tern/config"
	"github.com/axsh/arctic-tern/llmgateway"
	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/tasklog"
	"github.com/axsh/arctic-tern/vault"
	"github.com/axsh/arctic-tern/wsserver"
)

// Server is the tern core facade that orchestrates all components.
// Users interact with tern through this type.
type Server struct {
	cfg          *config.AppConfig
	logger       logger.Logger
	vault        vault.VaultStore
	gateway      llmgateway.LLMGatewayBackend
	agentService *agentservice.Server
	wsServer     *wsserver.Server
	taskLog      *tasklog.TaskLog
}

// New creates a new tern Server with the given options.
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
	cfg, configDir, err := resolveConfig(o)
	if err != nil {
		return nil, fmt.Errorf("tern: %w", err)
	}

	// Step 3: Resolve Logger.
	log := resolveLogger(o, cfg)

	if log != nil {
		log.Debug("resolving config", "config_path", o.configPath)
		log.Trace("config resolved",
			"gateway_port", cfg.LLMGateway.Port,
			"ws_port", cfg.WebSocket.Port,
			"agent_port", cfg.AgentService.Port,
			"vault_backend", cfg.Vault.Backend)
		log.Debug("resolving logger", "level", cfg.Log.Level)
		log.Debug("resolving vault", "backend", cfg.Vault.Backend)
	}

	// Step 4: Resolve VaultStore.
	vs := resolveVault(o, cfg)

	// Step 5: Resolve Gateway.
	if log != nil {
		gatewayType := "proxy"
		if cfg.LLMGateway.ModelProfilesPath != "" {
			gatewayType = "bifrost"
		}
		log.Debug("resolving gateway", "type", gatewayType, "port", cfg.LLMGateway.Port)
	}
	gw, err := resolveGateway(o, cfg, vs, log, configDir)
	if err != nil {
		return nil, fmt.Errorf("tern: %w", err)
	}

	tl := tasklog.New()

	// Build gateway URL from port for health check cascading.
	gatewayURL := ""
	if cfg.LLMGateway.Port > 0 {
		gatewayURL = fmt.Sprintf("http://localhost:%d", cfg.LLMGateway.Port)
	}

	as := resolveAgentService(o, log, tl, gatewayURL)

	wsPort := cfg.WebSocket.Port
	ws := wsserver.New(wsPort, tl, log)

	return &Server{
		cfg:          cfg,
		logger:       log,
		vault:        vs,
		gateway:      gw,
		agentService: as,
		wsServer:     ws,
		taskLog:      tl,
	}, nil
}

// Launch starts all components. Non-blocking.
// Start order: Gateway -> WebSocket -> AgentService.
func (s *Server) Launch(ctx context.Context) error {
	s.logger.Info("starting tern server")

	if err := s.gateway.Launch(ctx); err != nil {
		return fmt.Errorf("tern: gateway launch: %w", err)
	}
	s.logger.Debug("gateway launched", "port", s.cfg.LLMGateway.Port)

	if err := s.wsServer.Launch(ctx); err != nil {
		return fmt.Errorf("tern: wsserver launch: %w", err)
	}
	s.logger.Debug("websocket server launched", "port", s.cfg.WebSocket.Port)

	// AgentService HTTP server
	agentPort := s.cfg.AgentService.Port
	if agentPort == 0 {
		agentPort = 3100 // default port
	}
	if err := s.agentService.Launch(ctx, agentPort); err != nil {
		return fmt.Errorf("tern: agentservice launch: %w", err)
	}
	s.logger.Debug("agent service launched", "port", s.agentService.Port())

	s.logger.Info("tern server started")
	return nil
}

// Shutdown gracefully stops all components in reverse launch order.
// Stop order: AgentService -> WebSocket -> Gateway.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down tern server")

	if err := s.agentService.Shutdown(ctx); err != nil {
		return fmt.Errorf("tern: agentservice shutdown: %w", err)
	}

	if err := s.wsServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("tern: wsserver shutdown: %w", err)
	}

	if err := s.gateway.Shutdown(ctx); err != nil {
		return fmt.Errorf("tern: gateway shutdown: %w", err)
	}

	s.logger.Info("tern server stopped")
	return nil
}

// Gateway returns the LLM Gateway Proxy backend.
func (s *Server) Gateway() llmgateway.LLMGatewayBackend {
	return s.gateway
}

// AgentService returns the AgentService Server instance.
func (s *Server) AgentService() *agentservice.Server {
	return s.agentService
}

// TaskLog returns the TaskLog instance.
// Callers can use TaskLog().Add() to inject log entries.
func (s *Server) TaskLog() *tasklog.TaskLog {
	return s.taskLog
}

// WebSocketURL returns the WebSocket server URL.
// Returns empty string if the server has not been launched.
func (s *Server) WebSocketURL() string {
	return s.wsServer.URL()
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
// Returns the config and the directory containing the config file
// (used to resolve relative paths like model_profiles_path).
func resolveConfig(o *options) (*config.AppConfig, string, error) {
	if o.configPath != "" {
		cfg, err := config.Load(o.configPath)
		if err != nil {
			return nil, "", fmt.Errorf("load config from %s: %w", o.configPath, err)
		}
		absPath, _ := filepath.Abs(o.configPath)
		return cfg, filepath.Dir(absPath), nil
	}
	if o.cfg != nil {
		return o.cfg, "", nil
	}
	return &config.AppConfig{}, "", nil
}

// resolveLogger resolves the Logger from options.
// If WithLogger is set, use it. Otherwise create a default from Config.Log.
func resolveLogger(o *options, cfg *config.AppConfig) logger.Logger {
	if o.logger != nil {
		return o.logger
	}
	level := logger.ParseLevel(cfg.Log.Level)
	if len(cfg.Log.Outputs) > 0 {
		log, err := logger.BuildFromConfig(level, cfg.Log.Outputs)
		if err != nil {
			// Fallback to default if config is invalid.
			fallback := logger.NewDefault(level)
			fallback.Warn("failed to build logger from config, using default",
				"error", err.Error())
			return fallback
		}
		return log
	}
	return logger.NewDefault(level)
}

// resolveVault resolves the VaultStore from options.
// If WithVaultStore is set, use it. Otherwise select based on Config.Vault.Backend.
func resolveVault(o *options, cfg *config.AppConfig) vault.VaultStore {
	if o.vault != nil {
		return o.vault
	}
	switch cfg.Vault.Backend {
	case "keyring":
		return vault.NewKeyringVaultBackend()
	default:
		return vault.NewEnvVaultBackend()
	}
}

// resolveGateway resolves the LLMGatewayBackend from options.
// If WithGateway is set, use it. Otherwise:
//   - If model profiles are configured, create a BifrostDriver.
//   - Otherwise create a standalone ProxyServer.
//
// configDir is the directory containing config.yaml; relative
// model_profiles_path values are resolved against it.
func resolveGateway(o *options, cfg *config.AppConfig, vs vault.VaultStore, log logger.Logger, configDir string) (llmgateway.LLMGatewayBackend, error) {
	if o.gateway != nil {
		return o.gateway, nil
	}

	// If model profiles path is configured, try to load and use BifrostDriver.
	if cfg.LLMGateway.ModelProfilesPath != "" {
		profilesPath := cfg.LLMGateway.ModelProfilesPath
		// Resolve relative path against config directory.
		if !filepath.IsAbs(profilesPath) && configDir != "" {
			profilesPath = filepath.Join(configDir, profilesPath)
		}
		profiles, err := config.LoadModelProfiles(profilesPath)
		if err != nil {
			return nil, fmt.Errorf("load model profiles: %w", err)
		}
		return llmgateway.NewBifrostDriver(cfg, profiles, vs, log)
	}

	// No profiles configured; use standalone ProxyServer.
	return llmgateway.NewProxyServer(cfg, vs, log)
}

// resolveAgentService returns the externally provided AgentService or builds one.
func resolveAgentService(o *options, log logger.Logger, tl *tasklog.TaskLog, gatewayURL string) *agentservice.Server {
	if o.agentService != nil {
		return o.agentService
	}
	return agentservice.New(
		agentservice.WithLogger(log),
		agentservice.WithTaskLog(tl),
		agentservice.WithGatewayURL(gatewayURL),
	)
}

