// Package handlerctx defines shared types and interfaces for subpackage
// handlers (anthropic/, openai/) to access ProxyServer internals without
// creating circular imports with the parent llmgateway package.
package handlerctx

import (
	"encoding/json"
	"net/http"

	bifrost "github.com/maximhq/bifrost/core"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

// HandlerContext provides handler-level access to ProxyServer internals.
// Subpackage handlers receive this instead of a *ProxyServer reference,
// enabling handlers to live in separate packages without circular imports.
type HandlerContext interface {
	// Config returns the application config.
	Config() *config.AppConfig
	// Logger returns the logger instance.
	Logger() logger.Logger
	// Vault returns the vault store (may be nil).
	Vault() vault.VaultStore
	// Router returns the model router (may be nil).
	Router() ModelRouter
	// BifrostSDK returns the Bifrost SDK instance (may be nil).
	BifrostSDK() *bifrost.Bifrost
	// ToBifrostProvider converts a tern provider name to Bifrost ModelProvider.
	ToBifrostProvider(provider string) bifrostSchemas.ModelProvider
	// SanitizeTools filters tools in a Bifrost request for cross-provider compatibility.
	SanitizeTools(req *bifrostSchemas.BifrostResponsesRequest, provider bifrostSchemas.ModelProvider)
	// TryFallbackAnthropicResponse applies tool call fallback rewriting.
	TryFallbackAnthropicResponse(body []byte) ([]byte, bool)
	// ExtractSessionID extracts the session ID from an auth header value.
	ExtractSessionID(authHeader string) string
	// ExtractFallbackFlag extracts the fallback flag from an auth header value.
	ExtractFallbackFlag(authHeader string) bool
	// MaskSecret masks a secret string for logging.
	MaskSecret(s string) string
}

// ModelRouter resolves model names to routed model configurations.
type ModelRouter interface {
	// ResolveModel resolves a model name to a RoutedModel.
	ResolveModel(modelName string, sessionID string) (*RoutedModel, error)
}

// RoutedModel holds the routing result for a model request.
type RoutedModel struct {
	Provider         string `json:"provider"`           // e.g. "anthropic"
	KeyName          string `json:"key_name,omitempty"`  // e.g. "primary"
	KeyValue         string `json:"-"`                   // actual API key value from profile
	Model            string `json:"model"`               // e.g. "claude-sonnet-4-20250514"
	Mode             string `json:"mode,omitempty"`      // "chat", "responses", or "" (treated as "chat")
	ToolCallFallback bool   `json:"tool_call_fallback"`  // enable text-to-tool-call conversion
	MaxOutputTokens  int    `json:"max_output_tokens,omitempty"` // override default max_tokens
}

// GatewayError represents an API error response.
type GatewayError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"-"`
}

// Error implements the error interface.
func (e *GatewayError) Error() string {
	return e.Type + ": " + e.Message
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// WriteErrorResponse writes a JSON error response to the client.
func WriteErrorResponse(w http.ResponseWriter, err *GatewayError) {
	status := err.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorResponse{
		Error: errorBody{
			Type:    err.Type,
			Message: err.Message,
			Code:    err.Code,
		},
	})
}
