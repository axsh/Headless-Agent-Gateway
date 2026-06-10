package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/axsh/hag/vault"
)

// openaiRequest represents the minimal fields we parse from OpenAI Chat Completions API.
type openaiRequest struct {
	Model string `json:"model"`
}

// handleOpenAIChatCompletions handles POST /v1/chat/completions for OpenAI-compatible API.
// It routes the request through the model router and delegates to Bifrost SDK.
func (p *ProxyServer) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Read and parse the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "invalid_request_error",
			Message: "failed to read request body",
			Code:    "request_read_error",
			Status:  http.StatusBadRequest,
		})
		return
	}
	defer r.Body.Close()

	var req openaiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		})
		return
	}

	p.logger.Debug("openai request received", "method", r.Method, "path", r.URL.Path, "model", req.Model)

	// Route the model
	if p.driver == nil || p.driver.router == nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: "LLM gateway backend not configured",
			Code:    "not_configured",
			Status:  http.StatusServiceUnavailable,
		})
		return
	}

	sessionID := ExtractSessionID(r.Header.Get("Authorization"))

	routed, err := p.driver.router.ResolveModel(req.Model, sessionID)
	if err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "not_found_error",
			Message: "model not found: " + req.Model,
			Code:    "model_not_found",
			Status:  http.StatusNotFound,
		})
		return
	}

	// R8: OR fallback flag from Authorization header with model profile setting.
	if ExtractFallbackFlag(r.Header.Get("Authorization")) {
		routed.ToolCallFallback = true
	}

	p.logger.Debug("request routed", "model", routed.Model, "provider", routed.Provider, "mode", routed.Mode, "fallback", routed.ToolCallFallback)

	// Resolve vault reference if needed
	apiKey := routed.KeyValue
	if vault.IsVaultRef(apiKey) && p.vault != nil {
		resolved, err := p.vault.Resolve(apiKey)
		if err != nil {
			WriteErrorResponse(w, &GatewayError{
				Type:    "api_error",
				Message: "failed to resolve API key from vault",
				Code:    "vault_error",
				Status:  http.StatusInternalServerError,
			})
			return
		}
		apiKey = resolved
	}

	p.logger.Info("openai request routed",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", MaskSecret(apiKey),
	)

	// Rewrite model field in body if it has changed due to routing/fallback
	forwardBody := body
	if routed.Model != req.Model {
		forwardBody = rewriteModelField(body, req.Model, routed.Model)
	}

	bodyStr := string(body)
	if len(bodyStr) > 10240 {
		bodyStr = bodyStr[:10240] + "..."
	}
	p.logger.Trace("openai request body", "body", bodyStr)

	// Forward to upstream OpenAI API with retry.
	fwd := newProviderForwarder()
	retryCfg := p.buildRetryConfig()
	resp, err := fwd.forwardWithRetry(r.Context(), routed.Provider, "/v1/chat/completions", forwardBody, apiKey, r.Header, retryCfg, p.logger)
	if err != nil {
		if gwErr, ok := err.(*GatewayError); ok {
			WriteErrorResponse(w, gwErr)
		} else {
			WriteErrorResponse(w, &GatewayError{
				Type:    "api_error",
				Message: "upstream request failed: " + err.Error(),
				Code:    "upstream_error",
				Status:  http.StatusBadGateway,
			})
		}
		return
	}
	defer resp.Body.Close()

	p.logger.Debug("upstream response received", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))
	p.logger.Trace("upstream response headers", "headers", fmt.Sprintf("%+v", resp.Header))

	// Apply ToolCallFallback if enabled
	if routed.ToolCallFallback && resp.StatusCode == http.StatusOK && !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		respBody, err := io.ReadAll(resp.Body)
		if err == nil {
			rewritten, ok := TryFallbackOpenAIResponse(respBody)
			if ok {
				resp.Body = io.NopCloser(bytes.NewReader(rewritten))
				resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
			} else {
				resp.Body = io.NopCloser(bytes.NewReader(respBody))
			}
		}
	}

	proxyResponse(w, resp)
}
