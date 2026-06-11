package llmgateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/vault"
)

// anthropicRequest represents the minimal fields we parse from Anthropic Messages API.
type anthropicRequest struct {
	Model  string `json:"model"`
	Stream *bool  `json:"stream,omitempty"`
}

// handleAnthropicMessages handles POST /v1/messages for Anthropic-compatible API.
// It routes the request through the model router and delegates to Bifrost SDK.
// Falls back to legacy conversion path when Bifrost SDK is not initialized.
func (p *ProxyServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	// R5: Apply request body size limit.
	if maxBody := p.cfg.LLMGateway.MaxRequestBodyBytes; maxBody > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}

	// Read and parse the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			WriteErrorResponse(w, &GatewayError{
				Type:    "invalid_request_error",
				Message: "request body too large",
				Code:    "request_too_large",
				Status:  http.StatusRequestEntityTooLarge,
			})
			return
		}
		WriteErrorResponse(w, &GatewayError{
			Type:    "invalid_request_error",
			Message: "failed to read request body",
			Code:    "request_read_error",
			Status:  http.StatusBadRequest,
		})
		return
	}
	defer r.Body.Close()

	var req anthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		})
		return
	}

	p.logger.Debug("anthropic messages request received", "method", r.Method, "path", r.URL.Path, "model", req.Model)

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

	sessionID := ExtractSessionID(r.Header.Get("x-api-key"))

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

	// R8: OR fallback flag from x-api-key header with model profile setting.
	if ExtractFallbackFlag(r.Header.Get("x-api-key")) {
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

	p.logger.Info("anthropic request routed",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", MaskSecret(apiKey),
	)

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
	p.handleAnthropicMessagesViaBifrost(w, r, body, routed)
}

// handleAnthropicMessagesViaBifrost handles /v1/messages via Bifrost SDK.
// Converts the Anthropic request to BifrostResponsesRequest and delegates
// to Bifrost SDK for cross-provider translation.
func (p *ProxyServer) handleAnthropicMessagesViaBifrost(
	w http.ResponseWriter, r *http.Request,
	body []byte, routed *RoutedModel,
) {
	// Full-parse the request body for Bifrost conversion
	var fullReq AnthropicFullRequest
	if err := json.Unmarshal(body, &fullReq); err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		})
		return
	}

	providerKey := toBifrostProvider(routed.Provider)

	// Convert Anthropic -> Bifrost
	bifrostReq, err := ConvertAnthropicToBifrost(&fullReq, providerKey)
	if err != nil {
		p.logger.Error("failed to convert anthropic request to bifrost",
			"error", err, "model", routed.Model, "provider", routed.Provider)
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: "failed to convert request: " + err.Error(),
			Code:    "conversion_error",
			Status:  http.StatusInternalServerError,
		})
		return
	}

	// Override model with routing result
	bifrostReq.Model = routed.Model

	// Sanitize tools for cross-provider requests
	sanitizeToolsForProvider(bifrostReq, providerKey, p.logger)

	// Create Bifrost context
	bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)

	p.logger.Debug("anthropic request via bifrost",
		"provider", providerKey, "model", routed.Model,
		"stream", fullReq.Stream != nil && *fullReq.Stream)

	// Dispatch stream / non-stream
	if fullReq.Stream != nil && *fullReq.Stream {
		p.handleAnthropicMessagesBifrostStream(w, bifrostCtx, bifrostReq, routed.Model)
	} else {
		p.handleAnthropicMessagesBifrostNonStream(w, bifrostCtx, bifrostReq, routed)
	}
}

// handleAnthropicMessagesBifrostNonStream handles non-streaming /v1/messages via Bifrost SDK.
func (p *ProxyServer) handleAnthropicMessagesBifrostNonStream(
	w http.ResponseWriter,
	ctx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
	routed *RoutedModel,
) {
	p.logger.Debug("executing bifrost non-stream anthropic request", "model", req.Model)

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
		p.logger.Error("bifrost anthropic request failed",
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

	// Convert Bifrost response -> Anthropic response
	anthResp, err := ConvertBifrostToAnthropic(resp)
	if err != nil {
		p.logger.Error("failed to convert bifrost response to anthropic",
			"error", err, "model", req.Model)
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: "failed to convert response: " + err.Error(),
			Code:    "conversion_error",
			Status:  http.StatusInternalServerError,
		})
		return
	}

	// Apply ToolCallFallback if enabled
	if routed.ToolCallFallback {
		respJSON, _ := json.Marshal(anthResp)
		rewritten, ok := TryFallbackAnthropicResponse(respJSON)
		if ok {
			p.logger.Warn("tool call fallback applied", "model", routed.Model)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(rewritten)
			return
		}
	}

	p.logger.Debug("bifrost anthropic request succeeded", "model", req.Model)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(anthResp)
}

// handleAnthropicMessagesBifrostStream handles streaming /v1/messages via Bifrost SDK.
// Converts Bifrost stream chunks to Anthropic-compatible SSE events.
func (p *ProxyServer) handleAnthropicMessagesBifrostStream(
	w http.ResponseWriter,
	ctx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
	model string,
) {
	p.logger.Debug("executing bifrost stream anthropic request", "model", req.Model)

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
		p.logger.Error("bifrost stream anthropic request failed",
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

	// Emit Anthropic message_start event
	startMsg := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":          generateAnthropicID(),
			"type":        "message",
			"role":        "assistant",
			"model":       model,
			"content":     []any{},
			"stop_reason": nil,
			"usage":       map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	}
	emitSSEJSON(w, flusher, "message_start", startMsg)

	blockIndex := 0
	chunkCount := 0
	var totalOutputTokens int

	for chunk := range ch {
		if chunk == nil {
			continue
		}

		// Handle BifrostError chunks
		if chunk.BifrostError != nil {
			errJSON, _ := json.Marshal(chunk.BifrostError)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
			flusher.Flush()
			p.logger.Debug("bifrost stream error chunk sent", "model", model)
			continue
		}

		// Handle BifrostResponsesStreamResponse chunks
		if chunk.BifrostResponsesStreamResponse != nil {
			streamResp := chunk.BifrostResponsesStreamResponse
			chunkCount++

			switch streamResp.Type {
			case bifrostSchemas.ResponsesStreamResponseTypeOutputTextDelta:
				// Text delta -> content_block_delta
				if streamResp.Delta != nil {
					if chunkCount == 1 {
						// First text chunk: emit content_block_start
						emitSSEJSON(w, flusher, "content_block_start", map[string]any{
							"type":          "content_block_start",
							"index":         blockIndex,
							"content_block": map[string]any{"type": "text", "text": ""},
						})
					}
					emitSSEJSON(w, flusher, "content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": blockIndex,
						"delta": map[string]any{"type": "text_delta", "text": *streamResp.Delta},
					})
				}

			case bifrostSchemas.ResponsesStreamResponseTypeOutputTextDone:
				// Text done -> content_block_stop
				emitSSEJSON(w, flusher, "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				blockIndex++

			case bifrostSchemas.ResponsesStreamResponseTypeFunctionCallArgumentsDelta:
				// Function call arguments delta (tool_use in Anthropic stream)
				if streamResp.Delta != nil {
					emitSSEJSON(w, flusher, "content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": blockIndex,
						"delta": map[string]any{"type": "input_json_delta", "partial_json": *streamResp.Delta},
					})
				}

			case bifrostSchemas.ResponsesStreamResponseTypeFunctionCallArgumentsDone:
				// Function call done
				emitSSEJSON(w, flusher, "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				blockIndex++

			case bifrostSchemas.ResponsesStreamResponseTypeOutputItemAdded:
				// New output item -> may need content_block_start for tool_use
				if streamResp.Item != nil && streamResp.Item.Type != nil &&
					*streamResp.Item.Type == bifrostSchemas.ResponsesMessageTypeFunctionCall {
					toolName := ""
					toolID := ""
					if streamResp.Item.ResponsesToolMessage != nil {
						if streamResp.Item.ResponsesToolMessage.Name != nil {
							toolName = *streamResp.Item.ResponsesToolMessage.Name
						}
						if streamResp.Item.ResponsesToolMessage.CallID != nil {
							toolID = *streamResp.Item.ResponsesToolMessage.CallID
						}
					}
					emitSSEJSON(w, flusher, "content_block_start", map[string]any{
						"type":  "content_block_start",
						"index": blockIndex,
						"content_block": map[string]any{
							"type":  "tool_use",
							"id":    toolID,
							"name":  toolName,
							"input": map[string]any{},
						},
					})
				}

			case bifrostSchemas.ResponsesStreamResponseTypeCompleted:
				// Extract usage from completed response
				if streamResp.Response != nil && streamResp.Response.Usage != nil {
					totalOutputTokens = streamResp.Response.Usage.OutputTokens
				}

			default:
				// Skip other event types
			}
		}
	}

	// Emit message_delta with stop_reason
	emitSSEJSON(w, flusher, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
		"usage": map[string]any{"output_tokens": totalOutputTokens},
	})

	// Emit message_stop
	emitSSEJSON(w, flusher, "message_stop", map[string]any{
		"type": "message_stop",
	})

	p.logger.Debug("bifrost anthropic stream completed", "model", model, "chunks", chunkCount)
}

// emitSSEJSON writes an SSE event with a JSON data payload.
func emitSSEJSON(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	flusher.Flush()
}


