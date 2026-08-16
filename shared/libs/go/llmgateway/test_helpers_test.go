package llmgateway

import (
	"encoding/json"
	"errors"
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
)

// registerTestProviders registers all standard providers for testing.
// This is needed because provider subpackages (anthropic, openai, etc.)
// cannot be imported from the parent package due to import cycles.
// In production, the consuming binary imports these subpackages.
func registerTestProviders() {
	// Only register if not already registered (avoids panic on duplicate).
	providerMu.RLock()
	_, hasAnthropic := providerRegistry["anthropic"]
	providerMu.RUnlock()

	if hasAnthropic {
		return // Already registered.
	}

	RegisterProvider(&testAnthropicProvider{})
	RegisterProvider(&testOpenAIProvider{})
	RegisterProvider(&testGoogleProvider{})
	RegisterProvider(&testOllamaProvider{})

	// Register stub handlers for testing (mirrors subpackage init() behavior).
	registerTestHandlers()
}

// registerTestHandlers registers minimal handler stubs for testing.
// These stubs read JSON body and return appropriate errors, matching
// the behavior of the real subpackage handlers.
func registerTestHandlers() {
	handlerctx.RegisterHandler("POST /v1/messages", func(ctx handlerctx.HandlerContext) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			handleTestRequest(ctx, w, r)
		}
	})
	handlerctx.RegisterHandler("POST /v1/responses", func(ctx handlerctx.HandlerContext) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			handleTestRequest(ctx, w, r)
		}
	})
}

// handleTestRequest is a minimal handler that parses JSON and routes the model.
// It returns appropriate error responses for invalid JSON, unknown models, etc.
func handleTestRequest(ctx handlerctx.HandlerContext, w http.ResponseWriter, r *http.Request) {
	cfg := ctx.Config()
	if maxBody := cfg.LLMGateway.MaxRequestBodyBytes; maxBody > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}

	body, err := readBodyForTest(ctx, w, r)
	if err != nil {
		return
	}
	defer r.Body.Close()

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		}, ctx.Logger(), "path", r.URL.Path, "method", r.Method, "model", req.Model)
		return
	}

	router := ctx.Router()
	if router == nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: "LLM gateway backend not configured",
			Code:    "not_configured",
			Status:  http.StatusServiceUnavailable,
		}, ctx.Logger(), "path", r.URL.Path, "method", r.Method, "model", req.Model)
		return
	}

	sessionID := ctx.ExtractSessionID(r.Header.Get("x-api-key"))
	if sessionID == "" {
		sessionID = ctx.ExtractSessionID(r.Header.Get("Authorization"))
	}

	_, err = router.ResolveModel(req.Model, sessionID)
	if err != nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "not_found_error",
			Message: "model not found: " + req.Model,
			Code:    "model_not_found",
			Status:  http.StatusNotFound,
		}, ctx.Logger(), "path", r.URL.Path, "method", r.Method, "model", req.Model)
		return
	}

	bifrostSDK := ctx.BifrostSDK()
	if bifrostSDK == nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type: "api_error", Message: "Bifrost SDK not initialized", Code: "not_configured", Status: http.StatusServiceUnavailable,
		}, ctx.Logger(), "path", r.URL.Path, "method", r.Method, "model", req.Model)
		return
	}

	// Would normally call bifrost, but for stub tests this is unreachable
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"stub": true}`))
}

func readBodyForTest(ctx handlerctx.HandlerContext, w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body := make([]byte, 0)
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
					Type: "invalid_request_error", Message: "request body too large", Code: "request_too_large", Status: http.StatusRequestEntityTooLarge,
				}, ctx.Logger(), "path", r.URL.Path, "method", r.Method, "model", "")
				return nil, err
			}
			break
		}
	}
	return body, nil
}

// --- Test provider implementations (mirror subpackage behavior) ---

type testAnthropicProvider struct{}

func (p *testAnthropicProvider) Name() string    { return "anthropic" }
func (p *testAnthropicProvider) BaseURL() string { return "https://api.anthropic.com" }
func (p *testAnthropicProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
}
func (p *testAnthropicProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Anthropic
}

type testOpenAIProvider struct{}

func (p *testOpenAIProvider) Name() string    { return "openai" }
func (p *testOpenAIProvider) BaseURL() string { return "https://api.openai.com" }
func (p *testOpenAIProvider) SetAuthHeaders(req *http.Request, apiKey string, _ http.Header) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}
func (p *testOpenAIProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.OpenAI
}

type testGoogleProvider struct{}

func (p *testGoogleProvider) Name() string    { return "google" }
func (p *testGoogleProvider) BaseURL() string { return "https://generativelanguage.googleapis.com" }
func (p *testGoogleProvider) SetAuthHeaders(req *http.Request, apiKey string, _ http.Header) {
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Del("Authorization")
}
func (p *testGoogleProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Gemini
}

type testOllamaProvider struct{}

func (p *testOllamaProvider) Name() string    { return "ollama" }
func (p *testOllamaProvider) BaseURL() string { return "http://localhost:11434" }
func (p *testOllamaProvider) SetAuthHeaders(req *http.Request, apiKey string, _ http.Header) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}
func (p *testOllamaProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Ollama
}
