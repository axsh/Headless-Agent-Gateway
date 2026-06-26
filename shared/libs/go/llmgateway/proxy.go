package llmgateway

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

// ProxyServer implements LLMGatewayBackend with an HTTP proxy server.
type ProxyServer struct {
	cfg       *config.AppConfig
	profiles  *config.ModelProfilesConfig
	vault     vault.VaultStore
	logger    logger.Logger
	server    *http.Server
	listener  net.Listener
	port      int
	driver    *BifrostDriver  // back-reference for handler delegation (nil when standalone)
	authToken string          // R4: internal auth token
	tlsMgr    *TLSCertManager // R1: TLS cert manager (nil if TLS disabled)
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

	log.Debug("creating proxy server", "port", cfg.LLMGateway.Port)

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

// SetAuthToken sets the internal authentication token.
func (p *ProxyServer) SetAuthToken(token string) {
	p.authToken = token
}

// SetTLSCertManager sets the TLS certificate manager.
func (p *ProxyServer) SetTLSCertManager(mgr *TLSCertManager) {
	p.tlsMgr = mgr
}

// Launch starts the HTTP server on the configured port.
// If port is 0, an ephemeral port is used (useful for testing).
func (p *ProxyServer) Launch(_ context.Context) error {
	mux := http.NewServeMux()
	p.setupRoutes(mux)

	addr := fmt.Sprintf("127.0.0.1:%d", p.port)
	p.logger.Debug("proxy server listening", "addr", addr)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("llmgateway: listen %s: %w", addr, err)
	}
	p.listener = listener

	// Extract the actual port (important when port=0).
	p.port = listener.Addr().(*net.TCPAddr).Port

	serverCfg := p.cfg.LLMGateway.Server
	p.server = &http.Server{
		Handler:        mux,
		ReadTimeout:    time.Duration(serverCfg.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:   time.Duration(serverCfg.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:    time.Duration(serverCfg.IdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes: serverCfg.MaxHeaderBytes,
	}

	p.logger.Info("proxy server started", "port", p.port)

	if p.tlsMgr != nil {
		p.server.TLSConfig = &tls.Config{
			GetCertificate: p.tlsMgr.GetCertificate,
		}
		p.tlsMgr.Start()
		go func() {
			if err := p.server.ServeTLS(listener, "", ""); err != nil && err != http.ErrServerClosed {
				p.logger.Error("proxy server TLS error", "error", err)
			}
		}()
	} else {
		go func() {
			if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
				p.logger.Error("proxy server error", "error", err)
			}
		}()
	}

	return nil
}

// Shutdown gracefully stops the HTTP server.
func (p *ProxyServer) Shutdown(ctx context.Context) error {
	if p.tlsMgr != nil {
		p.tlsMgr.Stop()
	}
	if p.server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.logger.Debug("proxy server shutting down")
	p.logger.Info("proxy server shutting down")
	return p.server.Shutdown(ctx)
}

// ProxyURL returns the server URL.
func (p *ProxyServer) ProxyURL() string {
	scheme := "http"
	if p.tlsMgr != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://localhost:%d", scheme, p.port)
}

// ReloadProfiles updates the loaded model profiles at runtime.
func (p *ProxyServer) ReloadProfiles(profiles *config.ModelProfilesConfig) {
	p.profiles = profiles
	count := 0
	if profiles != nil {
		for _, provider := range profiles.Providers {
			for _, key := range provider.ApiKeys {
				count += len(key.Models)
			}
		}
	}
	p.logger.Info("model profiles reloaded", "count", count)
}

// ListModels returns model info from loaded profiles.
func (p *ProxyServer) ListModels() []ModelInfo {
	if p.profiles == nil {
		return []ModelInfo{}
	}

	var models []ModelInfo
	for providerName, provider := range p.profiles.Providers {
		for _, key := range provider.ApiKeys {
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

// DefaultModel returns the default model from profiles.
// Returns nil if no default profile is configured.
func (p *ProxyServer) DefaultModel() *ModelInfo {
	if p.profiles == nil {
		return nil
	}
	dp := p.profiles.DefaultProfile
	if dp.Provider == "" || dp.Model == "" {
		return nil
	}

	// Look up the model's behavior from provider config.
	info := &ModelInfo{
		Provider: dp.Provider,
		Model:    dp.Model,
	}
	if prov, ok := p.profiles.Providers[dp.Provider]; ok {
		for _, key := range prov.ApiKeys {
			for _, m := range key.Models {
				if m.Name == dp.Model {
					if m.Behavior != nil {
						info.ToolCallFallback = m.Behavior.ToolCallFallback
					}
					return info
				}
			}
		}
	}
	return info
}

// Health returns the proxy server health status.
func (p *ProxyServer) Health() HealthStatus {
	status := "ok"
	message := ""
	if p.tlsMgr != nil && p.tlsMgr.IsDegraded() {
		status = "degraded"
		if p.tlsMgr.IsExpired() {
			message = "TLS certificate expired -- restart the server to restore HTTPS"
		} else {
			message = "TLS certificate expiring soon -- auto-renewal in progress"
		}
	}
	return HealthStatus{
		Status:  status,
		Message: message,
		Models:  len(p.ListModels()),
	}
}

// authMiddleware handles token validation for protected endpoints.
func (p *ProxyServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.authToken == "" {
			next(w, r)
			return
		}
		// Check X-Gateway-Token header
		if token := r.Header.Get("X-Gateway-Token"); token == p.authToken {
			next(w, r)
			return
		}
		// Check x-api-key metadata (Claude Code)
		if extractToken(r.Header.Get("x-api-key")) == p.authToken {
			next(w, r)
			return
		}
		// Check Authorization metadata (Codex)
		if extractToken(r.Header.Get("Authorization")) == p.authToken {
			next(w, r)
			return
		}
		WriteErrorResponse(w, &GatewayError{
			Type:    "authentication_error",
			Message: "invalid or missing gateway token",
			Code:    "unauthorized",
			Status:  http.StatusUnauthorized,
		})
	}
}

func extractToken(headerValue string) string {
	if strings.HasPrefix(headerValue, "Bearer ") {
		headerValue = strings.TrimPrefix(headerValue, "Bearer ")
	}
	for _, part := range strings.Split(headerValue, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "token=") {
			return strings.TrimPrefix(part, "token=")
		}
	}
	return ""
}

// setupRoutes registers HTTP handlers on the given mux.
// Provider-specific handlers (anthropic, openai) are resolved from the handler
// registry populated by subpackage init() functions.
func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", p.handleIndex)
	mux.HandleFunc("GET /health", p.handleHealth)
	mux.HandleFunc("GET /v1/models", p.handleModels)

	// Register provider-specific handlers from the handler registry.
	for path, factory := range handlerctx.AllHandlers() {
		mux.HandleFunc(path, p.authMiddleware(factory(p)))
	}
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
			"POST /v1/responses",
		},
	})
}

type healthResponse struct {
	Status  string     `json:"status"`
	Message string     `json:"message,omitempty"`
	Models  int        `json:"models"`
	TLS     *tlsStatus `json:"tls,omitempty"`
}

type tlsStatus struct {
	Enabled       bool   `json:"enabled"`
	Mode          string `json:"mode"`
	CertExpiresAt string `json:"cert_expires_at,omitempty"`
	CertExpired   bool   `json:"cert_expired"`
}

// handleHealth returns the health status as JSON.
func (p *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	h := p.Health()
	resp := healthResponse{
		Status:  h.Status,
		Message: h.Message,
		Models:  h.Models,
	}
	if p.tlsMgr != nil {
		ts := &tlsStatus{
			Enabled:       true,
			Mode:          p.cfg.LLMGateway.TLS.Mode,
			CertExpiresAt: p.tlsMgr.ExpiresAt().Format(time.RFC3339),
			CertExpired:   p.tlsMgr.IsExpired(),
		}
		resp.TLS = ts
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleModels returns the list of configured models with optional default.
func (p *ProxyServer) handleModels(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"models": p.ListModels(),
	}
	if dm := p.DefaultModel(); dm != nil {
		resp["default_model"] = dm
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
