package llmgateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/vault"
)

// openaiRequest represents the minimal fields we parse from OpenAI Chat Completions API.
type openaiRequest struct {
	Model string `json:"model"`
}



// handleOpenAIResponses handles POST /v1/responses for OpenAI Responses API.
// This handler is used by Codex CLI in "responses" wire_api mode.
// It resolves the model via ModelRouter, and delegates to Bifrost SDK.
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

	// Full-parse into Bifrost's OpenAIResponsesRequest to enable typed conversion.
	// This populates Instructions, ToolChoice, Tools etc. in ResponsesParameters,
	// allowing Bifrost SDK's requestConverter to translate them to provider-native formats.
	var oaiReq bifrostOpenAI.OpenAIResponsesRequest
	if err := json.Unmarshal(body, &oaiReq); err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		})
		return
	}

	// Wrap model in openaiRequest for compatibility with routing and legacy fallback code.
	req := openaiRequest{Model: oaiReq.Model}

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

	// Bifrost SDK path (required)
	if p.driver.bifrostSDK == nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: "Bifrost SDK not initialized",
			Code:    "not_configured",
			Status:  http.StatusServiceUnavailable,
		})
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

	bodyStr := string(body)
	if len(bodyStr) > 10240 {
		bodyStr = bodyStr[:10240] + "..."
	}
	p.logger.Trace("openai responses request body", "body", bodyStr)

	// Build BifrostResponsesRequest via typed conversion path.
	// ToBifrostResponsesRequest populates Input and Params (Instructions, ToolChoice, Tools, etc.)
	// from the fully-parsed OpenAIResponsesRequest. Bifrost SDK's requestConverter then
	// translates these to provider-native formats (e.g. instructions -> SystemInstruction for Gemini).
	bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)
	bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)

	// Override provider and model with routing results.
	providerKey := toBifrostProvider(routed.Provider)
	bifrostReq.Provider = providerKey
	bifrostReq.Model = routed.Model

	// Sanitize tools for cross-provider requests.
	sanitizeToolsForProvider(bifrostReq, providerKey, p.logger)

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

// toBifrostProvider converts tern provider name to Bifrost ModelProvider.
// Uses the Provider Registry first, then falls back to static mapping.
func toBifrostProvider(provider string) bifrostSchemas.ModelProvider {
	if mp, ok := resolveProviderName(provider); ok {
		return mp
	}
	return bifrostSchemas.ModelProvider(provider)
}

