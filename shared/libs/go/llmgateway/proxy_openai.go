package llmgateway

import (
	"encoding/json"
	"io"
	"net/http"
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

	routed, err := p.driver.router.ResolveModel(req.Model)
	if err != nil {
		WriteErrorResponse(w, &GatewayError{
			Type:    "not_found_error",
			Message: "model not found: " + req.Model,
			Code:    "model_not_found",
			Status:  http.StatusNotFound,
		})
		return
	}

	p.logger.Info("openai request routed",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", MaskSecret(routed.KeyValue),
	)

	// TODO: Forward to Bifrost SDK when initialized (Part 3.5+)
	// For now, return service unavailable until Bifrost SDK is ready.
	WriteErrorResponse(w, &GatewayError{
		Type:    "api_error",
		Message: "Bifrost SDK not yet initialized. Awaiting credential configuration (Part 3.5).",
		Code:    "backend_not_ready",
		Status:  http.StatusServiceUnavailable,
	})
}
