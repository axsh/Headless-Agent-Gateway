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

// anthropicRequest represents the minimal fields we parse from Anthropic Messages API.
type anthropicRequest struct {
	Model string `json:"model"`
}

// handleAnthropicMessages handles POST /v1/messages for Anthropic-compatible API.
// It routes the request through the model router and delegates to Bifrost SDK.
func (p *ProxyServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
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

	// Determine forwarding path and body based on provider.
	var (
		forwardPath string
		forwardBody []byte
	)

	switch routed.Provider {
	case "anthropic":
		forwardPath = "/v1/messages"
		forwardBody = body
		if routed.Model != req.Model {
			forwardBody = rewriteModelField(body, req.Model, routed.Model)
		}
	case "openai":
		if routed.Mode == "responses" {
			// Responses API route for Codex and similar models.
			forwardPath = "/v1/responses"
			converted, convErr := ConvertAnthropicRequestToResponses(body)
			if convErr != nil {
				WriteErrorResponse(w, &GatewayError{
					Type:    "api_error",
					Message: "failed to convert request to Responses API format: " + convErr.Error(),
					Code:    "conversion_error",
					Status:  http.StatusInternalServerError,
				})
				return
			}
			forwardBody = converted
			p.logger.Info("cross-provider conversion", "direction", "anthropic->responses", "model", routed.Model)
		} else {
			// Chat Completions API route (default).
			forwardPath = "/v1/chat/completions"
			converted, convErr := ConvertAnthropicRequestToOpenAI(body)
			if convErr != nil {
				WriteErrorResponse(w, &GatewayError{
					Type:    "api_error",
					Message: "failed to convert request to OpenAI format: " + convErr.Error(),
					Code:    "conversion_error",
					Status:  http.StatusInternalServerError,
				})
				return
			}
			forwardBody = converted
			p.logger.Info("cross-provider conversion", "direction", "anthropic->openai", "model", routed.Model)
		}
	default:
		WriteErrorResponse(w, &GatewayError{
			Type:    "api_error",
			Message: "cross-provider translation not supported for: " + routed.Provider,
			Code:    "unsupported_translation",
			Status:  http.StatusBadRequest,
		})
		return
	}

	// Forward to upstream provider.
	fwd := newProviderForwarder()
	resp, err := fwd.forwardToProvider(routed.Provider, forwardPath, forwardBody, apiKey, r.Header)
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

	// Cross-provider response conversion (OpenAI -> Anthropic).
	if routed.Provider == "openai" && resp.StatusCode == http.StatusOK {
		if routed.Mode == "responses" {
			// Responses API response conversion.
			if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")
				w.WriteHeader(http.StatusOK)
				if streamErr := ConvertResponsesStreamToAnthropic(resp.Body, w, routed.Model); streamErr != nil {
					p.logger.Error("responses stream conversion error", "error", streamErr)
				}
				return
			}

			respBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				WriteErrorResponse(w, &GatewayError{
					Type:    "api_error",
					Message: "failed to read upstream response",
					Code:    "upstream_read_error",
					Status:  http.StatusBadGateway,
				})
				return
			}
			converted, convErr := ConvertResponsesResponseToAnthropic(respBody, routed.Model)
			if convErr != nil {
				WriteErrorResponse(w, &GatewayError{
					Type:    "api_error",
					Message: "failed to convert response from Responses API format: " + convErr.Error(),
					Code:    "conversion_error",
					Status:  http.StatusInternalServerError,
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(converted)
			return
		}

		// Chat Completions API response conversion (default).
		// Streaming: convert OpenAI SSE -> Anthropic SSE
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			if streamErr := ConvertOpenAIStreamToAnthropic(resp.Body, w, routed.Model); streamErr != nil {
				p.logger.Error("stream conversion error", "error", streamErr)
			}
			return
		}

		// Non-streaming: convert full response
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			WriteErrorResponse(w, &GatewayError{
				Type:    "api_error",
				Message: "failed to read upstream response",
				Code:    "upstream_read_error",
				Status:  http.StatusBadGateway,
			})
			return
		}
		converted, convErr := ConvertOpenAIResponseToAnthropic(respBody, routed.Model)
		if convErr != nil {
			WriteErrorResponse(w, &GatewayError{
				Type:    "api_error",
				Message: "failed to convert response from OpenAI format: " + convErr.Error(),
				Code:    "conversion_error",
				Status:  http.StatusInternalServerError,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(converted)
		return
	}

	// Apply ToolCallFallback if enabled (anthropic provider only).
	if routed.ToolCallFallback && resp.StatusCode == http.StatusOK && !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		respBody, err := io.ReadAll(resp.Body)
		if err == nil {
			rewritten, ok := TryFallbackAnthropicResponse(respBody)
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

