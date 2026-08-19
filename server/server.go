// Package server provides the tern (arctic-tern) core facade.
// Users interact with tern through the Server type, which orchestrates
// all components (LLM Gateway, Config, Vault, Logger).
package server

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	artifactstorage "github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
	"github.com/axsh/arctic-tern/shared/libs/go/wsserver"

	// Auto-register all built-in coding agents via init().
	_ "github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
	_ "github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	_ "github.com/axsh/arctic-tern/shared/libs/go/wayfinder"

	// Auto-register all built-in LLM providers via init().
	_ "github.com/axsh/arctic-tern/shared/libs/go/llmgateway/anthropic"
	_ "github.com/axsh/arctic-tern/shared/libs/go/llmgateway/google"
	_ "github.com/axsh/arctic-tern/shared/libs/go/llmgateway/ollama"
	_ "github.com/axsh/arctic-tern/shared/libs/go/llmgateway/openai"
)

// Server is the tern core facade that orchestrates all components.
// Users interact with tern through this type.
type Server struct {
	cfg           *config.AppConfig
	logger        logger.Logger
	vault         vault.VaultStore
	gateway       llmgateway.LLMGatewayBackend
	agentService  *agentservice.Server
	wsServer      *wsserver.Server
	taskLog       *tasklog.TaskLog
	gatewayToken  string                     // R4: generated or configured auth token
	tlsMgr        *llmgateway.TLSCertManager // R1: TLS manager
	artifactStore store.ArtifactStore        // nil until initialized in New()
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
			"vault_backends", cfg.Vault.Backends)
		log.Debug("resolving logger", "level", cfg.Log.Level)
		log.Debug("resolving vault", "backends", cfg.Vault.Backends)
	}

	// Step 4: Resolve VaultStore.
	vs, err := resolveVault(o, cfg, log)
	if err != nil {
		return nil, fmt.Errorf("tern: %w", err)
	}

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

	// Step 6: Resolve Auth Token (R4)
	gatewayToken := cfg.LLMGateway.AuthToken
	if gatewayToken == "" {
		tokenBytes := make([]byte, 32)
		if _, err := crypto_rand.Read(tokenBytes); err != nil {
			return nil, fmt.Errorf("tern: generate auth token: %w", err)
		}
		gatewayToken = hex.EncodeToString(tokenBytes)
		if log != nil {
			log.Debug("gateway auth token auto-generated", "token", gatewayToken)
		}
	}
	// Inject token into gateway
	if ps, ok := gw.(*llmgateway.ProxyServer); ok {
		ps.SetAuthToken(gatewayToken)
	} else if bd, ok := gw.(*llmgateway.BifrostDriver); ok {
		bd.SetAuthToken(gatewayToken)
	}

	// Step 7: Resolve TLS (R1)
	var tlsMgr *llmgateway.TLSCertManager
	if cfg.LLMGateway.TLS.Enabled {
		tlsMgr = llmgateway.NewTLSCertManager(cfg.LLMGateway.TLS, log)
		if cfg.LLMGateway.TLS.Mode == "auto" {
			if err := tlsMgr.GenerateAndLoad(); err != nil {
				return nil, fmt.Errorf("tern: generate TLS cert: %w", err)
			}
			if _, err := tlsMgr.WriteCACertFile(); err != nil {
				return nil, fmt.Errorf("tern: write CA cert: %w", err)
			}
		}
		if ps, ok := gw.(*llmgateway.ProxyServer); ok {
			ps.SetTLSCertManager(tlsMgr)
		} else if bd, ok := gw.(*llmgateway.BifrostDriver); ok {
			bd.SetTLSCertManager(tlsMgr)
		}
	}

	tl := tasklog.New()

	// Step 8: Validate API versions.
	supportedVersions := map[int]bool{1: true}
	enableVersions := o.enableVersions
	if len(enableVersions) == 0 {
		// Default: enable all supported versions.
		enableVersions = []int{1}
	}
	for _, v := range enableVersions {
		if !supportedVersions[v] {
			return nil, fmt.Errorf("tern: unsupported API version: %d", v)
		}
	}

	// Build gateway URL from port for health check cascading.
	gatewayURL := ""
	if cfg.LLMGateway.Port > 0 {
		scheme := "http"
		if tlsMgr != nil {
			scheme = "https"
		}
		gatewayURL = fmt.Sprintf("%s://localhost:%d", scheme, cfg.LLMGateway.Port)
	}

	// Inject TLS CA cert path to agentservice environment if TLS is enabled
	caCertPath := ""
	if tlsMgr != nil {
		caCertPath = tlsMgr.CACertFilePath()
	}

	// Initialize ArtifactStore and UserArtifactStorage using the config dir.
	var artifactSt store.ArtifactStore
	var userArtSt *artifactstorage.UserArtifactStorage
	artifactWorkDir := ""
	if configDir != "" {
		dbDir := filepath.Join(configDir, "artifacts")
		if err := os.MkdirAll(dbDir, 0o755); err == nil {
			if st, err := store.NewSQLiteStore(filepath.Join(dbDir, "artifacts.db")); err == nil {
				artifactSt = st
				if log != nil {
					log.Info("artifact store initialized", "path", filepath.Join(dbDir, "artifacts.db"))
				}
			} else if log != nil {
				log.Warn("failed to initialize artifact store", "error", err.Error())
			}
		}
		userFilesDir := filepath.Join(configDir, "user-artifacts")
		if st, err := artifactstorage.New(userFilesDir); err == nil {
			userArtSt = st
		} else if log != nil {
			log.Warn("failed to initialize user artifact storage", "error", err.Error())
		}
	}

	as := resolveAgentService(o, log, tl, gatewayURL, gatewayToken, caCertPath, gw, cfg, configDir, cfg.AgentService.DisableSandbox, cfg.AgentService.EnableSubagent, artifactSt, artifactWorkDir, userArtSt)
	as.SetEnabledVersions(enableVersions)

	wsPort := cfg.WebSocket.Port
	ws := wsserver.New(wsPort, tl, log)

	return &Server{
		cfg:           cfg,
		logger:        log,
		vault:         vs,
		gateway:       gw,
		agentService:  as,
		wsServer:      ws,
		taskLog:       tl,
		gatewayToken:  gatewayToken,
		tlsMgr:        tlsMgr,
		artifactStore: artifactSt,
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

	// Close artifact store (releases the SQLite file lock).
	if s.artifactStore != nil {
		_ = s.artifactStore.Close()
	}

	s.logger.Info("tern server stopped")
	return nil
}

// closeArtifactStore closes the artifact store if it is open.
// This is a thin helper used in tests to release file locks without a full Shutdown.
func (s *Server) closeArtifactStore() error {
	if s.artifactStore != nil {
		return s.artifactStore.Close()
	}
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

// GatewayToken returns the internal gateway auth token.
func (s *Server) GatewayToken() string {
	return s.gatewayToken
}

// TLSCACertPath returns the CA certificate file path (for agent CLI).
// Empty if TLS is disabled.
func (s *Server) TLSCACertPath() string {
	if s.tlsMgr != nil {
		return s.tlsMgr.CACertFilePath()
	}
	return ""
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
// If WithVaultStore is set, use it. Otherwise build from Config.Vault.Backends.
func resolveVault(o *options, cfg *config.AppConfig, log logger.Logger) (vault.VaultStore, error) {
	// 1. WithVaultStore option takes priority.
	if o.vault != nil {
		return o.vault, nil
	}

	// 2. Detect deprecated backend (singular) field (R5).
	if cfg.Vault.Backend != "" {
		return nil, fmt.Errorf(
			"vault.backend (singular) is no longer supported. "+
				"Use vault.backends (plural) instead.\n\n"+
				"Migration example:\n"+
				"  # Before\n"+
				"  vault:\n"+
				"    backend: %s\n\n"+
				"  # After\n"+
				"  vault:\n"+
				"    backends: [%s]",
			cfg.Vault.Backend, cfg.Vault.Backend)
	}

	// 3. Validate backends is set and non-empty (R2).
	if len(cfg.Vault.Backends) == 0 {
		return nil, fmt.Errorf(
			"vault.backends is required but not configured.\n\n" +
				"Add a 'backends' list to the vault section of your config file to specify\n" +
				"which secret backends to use and in what order they should be tried.\n\n" +
				"Example configurations:\n\n" +
				"  # Use OS keyring (recommended for desktop environments)\n" +
				"  vault:\n" +
				"    backends: [keyring]\n\n" +
				"  # Use OS keyring first, fall back to environment variables\n" +
				"  vault:\n" +
				"    backends: [keyring, env]\n\n" +
				"  # Use encrypted file backend\n" +
				"  vault:\n" +
				"    backends: [file]\n" +
				"    file_path: /path/to/secrets.json\n\n" +
				"Supported backends: keyring, env, file")
	}

	// 4. Build each backend and validate (R3).
	supported := map[string]bool{"env": true, "keyring": true, "file": true}
	names := make([]string, 0, len(cfg.Vault.Backends))
	stores := make([]vault.VaultStore, 0, len(cfg.Vault.Backends))

	for _, name := range cfg.Vault.Backends {
		if !supported[name] {
			return nil, fmt.Errorf(
				"unsupported vault backend %q in vault.backends.\n\n"+
					"Supported backends: keyring, env, file\n\n"+
					"Check your config file for typos in the vault.backends list.",
				name)
		}
		switch name {
		case "env":
			stores = append(stores, vault.NewEnvVaultBackend())
		case "keyring":
			stores = append(stores, vault.NewKeyringVaultBackend())
		case "file":
			if cfg.Vault.FilePath == "" {
				return nil, fmt.Errorf(
					"vault backend \"file\" requires vault.file_path to be set.\n\n" +
						"Example:\n" +
						"  vault:\n" +
						"    backends: [file]\n" +
						"    file_path: /path/to/secrets.json")
			}
			fb, err := vault.NewFileVaultBackend(cfg.Vault.FilePath)
			if err != nil {
				return nil, fmt.Errorf("vault file backend: %w", err)
			}
			stores = append(stores, fb)
		}
		names = append(names, name)
	}

	// 5. If only one backend, return it directly (no chain overhead).
	if len(stores) == 1 {
		return stores[0], nil
	}

	// 6. Build chain (R4).
	return vault.NewChainVaultBackend(names, stores, log), nil
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
// When building internally, it also auto-registers all coding agents that
// self-registered via init() in the codingagent global registry.
func resolveAgentService(o *options, log logger.Logger, tl *tasklog.TaskLog, gatewayURL string, gatewayToken string, caCertPath string, gw llmgateway.LLMGatewayBackend, cfg *config.AppConfig, configDir string, disableSandbox bool, enableSubagent bool, artifactSt store.ArtifactStore, artifactWorkDir string, userArtSt *artifactstorage.UserArtifactStorage) *agentservice.Server {
	if o.agentService != nil {
		return o.agentService
	}

	if strings.HasPrefix(gatewayURL, "https://") && caCertPath != "" {
		os.Setenv("NODE_EXTRA_CA_CERTS", caCertPath)
		if log != nil {
			log.Debug("set NODE_EXTRA_CA_CERTS env var", "path", caCertPath)
		}
	}

	asOpts := []agentservice.ServerOption{
		agentservice.WithLogger(log),
		agentservice.WithTaskLog(tl),
		agentservice.WithGatewayURL(gatewayURL),
		agentservice.WithGatewayToken(gatewayToken),
		agentservice.WithSandboxDisabled(disableSandbox),
		agentservice.WithSubagentEnabled(enableSubagent),
	}
	if cfg != nil {
		cfg.AgentService.ApplyDefaults()
		asOpts = append(asOpts, agentservice.WithSupplementConfig(cfg.AgentService.Supplement))
		asOpts = append(asOpts, agentservice.WithProcessRetry(cfg.AgentService.ProcessRetry))
		asOpts = append(asOpts, agentservice.WithSSEDrainTimeout(
			time.Duration(cfg.AgentService.SSEReattachTimeoutSeconds)*time.Second))
	}
	if artifactSt != nil {
		asOpts = append(asOpts, agentservice.WithArtifactStore(artifactSt, artifactWorkDir))
	}
	if userArtSt != nil {
		asOpts = append(asOpts, agentservice.WithArtifactStorage(userArtSt))
	}
	as := agentservice.New(asOpts...)

	if cfg != nil && cfg.LLMGateway.ModelProfilesPath != "" {
		profilesPath := cfg.LLMGateway.ModelProfilesPath
		if !filepath.IsAbs(profilesPath) && configDir != "" {
			profilesPath = filepath.Join(configDir, profilesPath)
		}
		if profiles, err := config.LoadModelProfiles(profilesPath); err != nil {
			if log != nil {
				log.Warn("failed to load model profiles for agent service", "error", err.Error(), "path", profilesPath)
			}
		} else {
			as.SetModelProfiles(profiles)
		}
	}

	// Auto-register coding agents from the global registry.
	defaultModel := ""
	toolCallFallback := false
	if gw != nil {
		if dm := gw.DefaultModel(); dm != nil {
			defaultModel = dm.Model
			toolCallFallback = dm.ToolCallFallback
		}
	}

	adapterCfg := &codingagent.AdapterConfig{
		GatewayURL:       gatewayURL,
		GatewayToken:     gatewayToken,
		Logger:           log,
		DefaultModel:     defaultModel,
		ToolCallFallback: toolCallFallback,
		DisableSandbox:   disableSandbox,
		EnableSubagent:   enableSubagent,
	}

	for _, agent := range codingagent.CreateAll(adapterCfg) {
		as.RegisterAgent(agent)
		if log != nil {
			log.Debug("auto-registered coding agent", "agent", agent.Name())
		}
	}

	return as
}
