package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

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

// handleOpenAIResponses handles POST /v1/responses for OpenAI Responses API.
// This handler is used by Codex CLI in "responses" wire_api mode.
// It resolves the model via ModelRouter, and delegates to Bifrost SDK for
// provider-specific conversion (OpenAI, Gemini, Anthropic).
// Falls back to legacy passthrough when Bifrost SDK is not initialized.
func (p *ProxyServer) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
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

	p.logger.Debug("openai responses request received", "method", r.Method, "path", r.URL.Path, "model", req.Model)

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

	p.logger.Debug("responses request routed", "model", routed.Model, "provider", routed.Provider, "mode", routed.Mode)

	// Fallback to legacy passthrough when Bifrost SDK is not available.
	if p.driver.bifrostSDK == nil {
		p.logger.Debug("bifrost SDK not available, using legacy forwarder")
		p.handleOpenAIResponsesLegacy(w, r, body, req, routed)
		return
	}

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

	p.logger.Info("openai responses request via bifrost",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", MaskSecret(apiKey),
	)

	// Rewrite model field in body if it has changed due to routing
	forwardBody := body
	if routed.Model != req.Model {
		forwardBody = rewriteModelField(body, req.Model, routed.Model)
	}

	bodyStr := string(body)
	if len(bodyStr) > 10240 {
		bodyStr = bodyStr[:10240] + "..."
	}
	p.logger.Trace("openai responses request body", "body", bodyStr)

	// Build BifrostResponsesRequest with raw body passthrough.
	providerKey := toBifrostProvider(routed.Provider)
	bifrostReq := &bifrostSchemas.BifrostResponsesRequest{
		Provider:       providerKey,
		Model:          routed.Model,
		RawRequestBody: forwardBody,
	}

	// Build BifrostContext with raw body mode enabled.
	bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)
	bifrostCtx.SetValue(bifrostSchemas.BifrostContextKeyUseRawRequestBody, true)

	p.logger.Debug("bifrost request constructed",
		"provider", providerKey, "model", routed.Model,
		"stream", isStreamRequest(body))

	// Dispatch to stream or non-stream handler.
	if isStreamRequest(body) {
		p.handleOpenAIResponsesStream(w, bifrostCtx, bifrostReq)
	} else {
		p.handleOpenAIResponsesNonStream(w, bifrostCtx, bifrostReq)
	}
}

// handleOpenAIResponsesNonStream handles non-streaming Responses API requests via Bifrost SDK.
func (p *ProxyServer) handleOpenAIResponsesNonStream(
	w http.ResponseWriter,
	ctx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
) {
	p.logger.Debug("executing bifrost non-stream responses request", "model", req.Model)

	resp, bifrostErr := p.driver.bifrostSDK.ResponsesRequest(ctx, req)
	if bifrostErr != nil {
		status := http.StatusBadGateway
		if bifrostErr.StatusCode != nil {
			status = *bifrostErr.StatusCode
		}
		msg := "upstream request failed"
		if bifrostErr.Error != nil {
			msg = bifrostErr.Error.Message
		}
		p.logger.Error("bifrost responses request failed",
			"status", status, "message", msg,
			"model", req.Model, "provider", req.Provider)
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: msg,
			Code:    "upstream_error",
			Status:  status,
		})
		return
	}

	p.logger.Debug("bifrost responses request succeeded", "model", req.Model)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleOpenAIResponsesStream handles streaming Responses API requests via Bifrost SDK.
// Converts Bifrost's channel-based streaming to SSE (Server-Sent Events) format.
func (p *ProxyServer) handleOpenAIResponsesStream(
	w http.ResponseWriter,
	ctx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
) {
	p.logger.Debug("executing bifrost stream responses request", "model", req.Model)

	ch, bifrostErr := p.driver.bifrostSDK.ResponsesStreamRequest(ctx, req)
	if bifrostErr != nil {
		status := http.StatusBadGateway
		if bifrostErr.StatusCode != nil {
			status = *bifrostErr.StatusCode
		}
		msg := "upstream stream request failed"
		if bifrostErr.Error != nil {
			msg = bifrostErr.Error.Message
		}
		p.logger.Error("bifrost stream responses request failed",
			"status", status, "message", msg,
			"model", req.Model, "provider", req.Provider)
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: msg,
			Code:    "upstream_error",
			Status:  status,
		})
		return
	}

	// Set SSE response headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		p.logger.Error("response writer does not support flushing for SSE")
		return
	}

	chunkCount := 0
	for chunk := range ch {
		if chunk == nil {
			continue
		}

		// Handle BifrostError chunks.
		if chunk.BifrostError != nil {
			errJSON, _ := json.Marshal(chunk.BifrostError)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
			flusher.Flush()
			p.logger.Debug("bifrost stream error chunk sent", "model", req.Model)
			continue
		}

		// Handle BifrostResponsesStreamResponse chunks.
		if chunk.BifrostResponsesStreamResponse != nil {
			data, err := json.Marshal(chunk.BifrostResponsesStreamResponse)
			if err != nil {
				p.logger.Error("failed to marshal stream chunk", "error", err)
				continue
			}
			eventType := string(chunk.BifrostResponsesStreamResponse.Type)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
			flusher.Flush()
			chunkCount++
		}
	}

	p.logger.Debug("bifrost stream completed", "model", req.Model, "chunks", chunkCount)
}

// handleOpenAIResponsesLegacy is the original passthrough implementation.
// Used when Bifrost SDK is not initialized (fallback path).
func (p *ProxyServer) handleOpenAIResponsesLegacy(
	w http.ResponseWriter, r *http.Request,
	body []byte, req openaiRequest, routed *RoutedModel,
) {
	p.logger.Debug("using legacy forwarder for responses request", "model", routed.Model, "provider", routed.Provider)

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

	p.logger.Info("openai responses request routed (legacy)",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", MaskSecret(apiKey),
	)

	// Rewrite model field in body if it has changed due to routing
	forwardBody := body
	if routed.Model != req.Model {
		forwardBody = rewriteModelField(body, req.Model, routed.Model)
	}

	bodyStr := string(body)
	if len(bodyStr) > 10240 {
		bodyStr = bodyStr[:10240] + "..."
	}
	p.logger.Trace("openai responses request body", "body", bodyStr)

	// Forward to upstream OpenAI Responses API with retry.
	fwd := newProviderForwarder()
	retryCfg := p.buildRetryConfig()
	resp, err := fwd.forwardWithRetry(r.Context(), routed.Provider, "/v1/responses", forwardBody, apiKey, r.Header, retryCfg, p.logger)
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

	p.logger.Debug("upstream responses response received (legacy)", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))

	proxyResponse(w, resp)
}

// isStreamRequest checks if the request body has "stream": true.
func isStreamRequest(body []byte) bool {
	var raw struct {
		Stream *bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}
	return raw.Stream != nil && *raw.Stream
}

// toBifrostProvider converts HAG provider name to Bifrost ModelProvider.
// Uses the providerNameMap defined in bifrost_account.go.
func toBifrostProvider(provider string) bifrostSchemas.ModelProvider {
	if mp, ok := providerNameMap[provider]; ok {
		return mp
	}
	return bifrostSchemas.ModelProvider(provider)
}

