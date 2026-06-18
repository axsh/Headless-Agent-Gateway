// Package llmgateway provides the LLM Gateway Proxy backend interface and implementations.
package llmgateway

import "context"

// LLMGatewayBackend is the interface for LLM proxy backends.
// Implementations include ProxyServer (HTTP proxy with Bifrost) and StubGateway (testing).
type LLMGatewayBackend interface {
	// Launch starts the HTTP proxy server. Non-blocking.
	Launch(ctx context.Context) error

	// Shutdown gracefully stops the HTTP proxy server.
	Shutdown(ctx context.Context) error

	// ListModels returns the list of configured models.
	ListModels() []ModelInfo

	// DefaultModel returns the default model from model profiles.
	// Returns nil if no default profile is configured.
	DefaultModel() *ModelInfo

	// Health returns the backend health status.
	Health() HealthStatus

	// ProxyURL returns the HTTP proxy URL for agent CLI injection.
	ProxyURL() string
}

// ModelInfo describes a configured model.
type ModelInfo struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	ToolCallFallback bool   `json:"tool_call_fallback,omitempty"`
}

// HealthStatus describes the backend health.
type HealthStatus struct {
	Status  string `json:"status"` // "ok", "degraded", "down", "stub"
	Message string `json:"message,omitempty"`
	Models  int    `json:"models"` // number of configured models
}
