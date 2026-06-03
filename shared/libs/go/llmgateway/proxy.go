package llmgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/vault"
)

// ProxyServer implements LLMGatewayBackend with an HTTP proxy server.
type ProxyServer struct {
	cfg      *config.AppConfig
	profiles *config.ModelProfilesConfig
	vault    vault.VaultStore
	logger   logger.Logger
	server   *http.Server
	listener net.Listener
	port     int
	driver   *BifrostDriver // back-reference for handler delegation (nil when standalone)
}

// NewProxyServer creates a ProxyServer.
// cfg, vs, and log may be nil; defaults will be used.
// If cfg.LLMGateway.ModelProfilesPath is set, model profiles are loaded.
func NewProxyServer(cfg *config.AppConfig, vs vault.VaultStore, log logger.Logger) (*ProxyServer, error) {
	if cfg == nil {
		cfg = &config.AppConfig{}
	}
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}

	p := &ProxyServer{
		cfg:    cfg,
		vault:  vs,
		logger: log.WithComponent("llmgateway"),
		port:   cfg.LLMGateway.Port,
	}

	// Load model profiles if path is configured.
	if cfg.LLMGateway.ModelProfilesPath != "" {
		profiles, err := config.LoadModelProfiles(cfg.LLMGateway.ModelProfilesPath)
		if err != nil {
			return nil, fmt.Errorf("llmgateway: load model profiles: %w", err)
		}
		p.profiles = profiles
	}

	return p, nil
}

// Launch starts the HTTP server on the configured port.
// If port is 0, an ephemeral port is used (useful for testing).
func (p *ProxyServer) Launch(_ context.Context) error {
	mux := http.NewServeMux()
	p.setupRoutes(mux)

	addr := fmt.Sprintf(":%d", p.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("llmgateway: listen %s: %w", addr, err)
	}
	p.listener = listener

	// Extract the actual port (important when port=0).
	p.port = listener.Addr().(*net.TCPAddr).Port

	p.server = &http.Server{Handler: mux}

	p.logger.Info("proxy server started", "port", p.port)

	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.logger.Error("proxy server error", "error", err)
		}
	}()

	return nil
}

// Shutdown gracefully stops the HTTP server.
func (p *ProxyServer) Shutdown(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.logger.Info("proxy server shutting down")
	return p.server.Shutdown(ctx)
}

// ProxyURL returns "http://localhost:{port}".
func (p *ProxyServer) ProxyURL() string {
	return fmt.Sprintf("http://localhost:%d", p.port)
}

// ReloadProfiles updates the loaded model profiles at runtime.
func (p *ProxyServer) ReloadProfiles(profiles *config.ModelProfilesConfig) {
	p.profiles = profiles
}

// ListModels returns model info from loaded profiles.
func (p *ProxyServer) ListModels() []ModelInfo {
	if p.profiles == nil {
		return []ModelInfo{}
	}

	var models []ModelInfo
	for providerName, provider := range p.profiles.Providers {
		for _, key := range provider.Keys {
			for _, model := range key.Models {
				models = append(models, ModelInfo{
					Provider: providerName,
					Model:    model.Name,
				})
			}
		}
	}
	return models
}

// Health returns the proxy server health status.
func (p *ProxyServer) Health() HealthStatus {
	return HealthStatus{
		Status: "ok",
		Models: len(p.ListModels()),
	}
}

// setupRoutes registers HTTP handlers on the given mux.
func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", p.handleIndex)
	mux.HandleFunc("GET /health", p.handleHealth)
	mux.HandleFunc("GET /v1/models", p.handleModels)
	mux.HandleFunc("POST /v1/messages", p.handleAnthropicMessages)
	mux.HandleFunc("POST /v1/chat/completions", p.handleOpenAIChatCompletions)
}

// handleIndex returns 200 OK with endpoint list (Claude Code reachability check).
func (p *ProxyServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"endpoints": []string{
			"GET /",
			"GET /health",
			"GET /v1/models",
			"POST /v1/messages",
			"POST /v1/chat/completions",
		},
	})
}

// handleHealth returns the health status as JSON.
func (p *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p.Health())
}

// handleModels returns the list of configured models.
func (p *ProxyServer) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"models": p.ListModels(),
	})
}

