package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

// request represents the minimal fields we parse from OpenAI Chat Completions API.
type request struct {
	Model string `json:"model"`
}

// HandleResponses returns an http.HandlerFunc that handles POST /v1/responses
// for OpenAI Responses API. This handler is used by Codex CLI in "responses"
// wire_api mode.
func HandleResponses(ctx handlerctx.HandlerContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleResponses(ctx, w, r)
	}
}

// handleResponses handles POST /v1/responses for OpenAI Responses API.
func handleResponses(ctx handlerctx.HandlerContext, w http.ResponseWriter, r *http.Request) {
	cfg := ctx.Config()
	log := ctx.Logger()

	// R5: Apply request body size limit.
	if maxBody := cfg.LLMGateway.MaxRequestBodyBytes; maxBody > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}

	// Read and parse the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
				Type:    "invalid_request_error",
				Message: "request body too large",
				Code:    "request_too_large",
				Status:  http.StatusRequestEntityTooLarge,
			})
			return
		}
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "failed to read request body",
			Code:    "request_read_error",
			Status:  http.StatusBadRequest,
		})
		return
	}
	defer r.Body.Close()

	// Full-parse into Bifrost's OpenAIResponsesRequest to enable typed conversion.
	var oaiReq bifrostOpenAI.OpenAIResponsesRequest
	if err := json.Unmarshal(body, &oaiReq); err != nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		})
		return
	}

	// Wrap model in request for compatibility with routing and legacy fallback code.
	req := request{Model: oaiReq.Model}

	log.Debug("openai responses request received", "method", r.Method, "path", r.URL.Path, "model", req.Model)

	// Route the model
	router := ctx.Router()
	if router == nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: "LLM gateway backend not configured",
			Code:    "not_configured",
			Status:  http.StatusServiceUnavailable,
		})
		return
	}

	sessionID := ctx.ExtractSessionID(r.Header.Get("Authorization"))

	routed, err := router.ResolveModel(req.Model, sessionID)
	if err != nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "not_found_error",
			Message: "model not found: " + req.Model,
			Code:    "model_not_found",
			Status:  http.StatusNotFound,
		})
		return
	}

	log.Debug("responses request routed", "model", routed.Model, "provider", routed.Provider, "mode", routed.Mode)

	// Bifrost SDK path (required)
	bifrostSDK := ctx.BifrostSDK()
	if bifrostSDK == nil {
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: "Bifrost SDK not initialized",
			Code:    "not_configured",
			Status:  http.StatusServiceUnavailable,
		})
		return
	}

	// Resolve vault reference if needed
	apiKey := routed.KeyValue
	if vault.IsVaultRef(apiKey) && ctx.Vault() != nil {
		resolved, err := ctx.Vault().Resolve(apiKey)
		if err != nil {
			handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
				Type:    "api_error",
				Message: "failed to resolve API key from vault",
				Code:    "vault_error",
				Status:  http.StatusInternalServerError,
			})
			return
		}
		apiKey = resolved
	}

	log.Info("openai responses request via bifrost",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", ctx.MaskSecret(apiKey),
	)

	bodyStr := string(body)
	if len(bodyStr) > 10240 {
		bodyStr = bodyStr[:10240] + "..."
	}
	log.Trace("openai responses request body", "body", bodyStr)

	// Build BifrostResponsesRequest via typed conversion path.
	bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)
	bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)

	// Override provider and model with routing results.
	providerKey := ctx.ToBifrostProvider(routed.Provider)
	bifrostReq.Provider = providerKey
	bifrostReq.Model = routed.Model

	// Sanitize tools for cross-provider requests.
	ctx.SanitizeTools(bifrostReq, providerKey)

	log.Debug("bifrost request constructed",
		"provider", providerKey, "model", routed.Model,
		"stream", isStreamRequest(body))

	// Dispatch to stream or non-stream handler.
	if isStreamRequest(body) {
		handleResponsesStream(ctx, w, r.Context(), bifrostCtx, bifrostReq)
	} else {
		handleResponsesNonStream(ctx, w, r.Context(), bifrostCtx, bifrostReq)
	}
}

func bifrostErrorMessage(berr *bifrostSchemas.BifrostError, fallback string) string {
	if berr != nil && berr.Error != nil && berr.Error.Message != "" {
		return berr.Error.Message
	}
	return fallback
}

func openResponsesStream(
	reqCtx context.Context,
	hctx handlerctx.HandlerContext,
	budget *llmgateway.RetryBudget,
	bCtx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
) (chan *bifrostSchemas.BifrostStreamChunk, error) {
	return llmgateway.OpenWithBudget(budget, reqCtx, hctx.Logger(), func() (chan *bifrostSchemas.BifrostStreamChunk, error) {
		ch, berr := hctx.BifrostSDK().ResponsesStreamRequest(bCtx, req)
		if berr != nil {
			return nil, llmgateway.StreamErr(bifrostErrorMessage(berr, "upstream stream request failed"))
		}
		return ch, nil
	})
}

// handleResponsesNonStream handles non-streaming Responses API requests via Bifrost SDK.
func handleResponsesNonStream(
	ctx handlerctx.HandlerContext,
	w http.ResponseWriter,
	reqCtx context.Context,
	bCtx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
) {
	log := ctx.Logger()
	log.Debug("executing bifrost non-stream responses request", "model", req.Model)

	var resp *bifrostSchemas.BifrostResponsesResponse
	err := llmgateway.DoWithRetry(reqCtx, ctx.Config().LLMGateway.Retry, log, func() error {
		var berr *bifrostSchemas.BifrostError
		resp, berr = ctx.BifrostSDK().ResponsesRequest(bCtx, req)
		if berr != nil {
			return llmgateway.StreamErr(bifrostErrorMessage(berr, "upstream request failed"))
		}
		return nil
	})
	if err != nil {
		log.Error("bifrost responses request failed",
			"message", err.Error(),
			"model", req.Model, "provider", req.Provider)
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: err.Error(),
			Code:    "upstream_error",
			Status:  http.StatusBadGateway,
		})
		return
	}

	log.Debug("bifrost responses request succeeded", "model", req.Model)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleResponsesStream handles streaming Responses API requests via Bifrost SDK.
// Converts Bifrost's channel-based streaming to SSE (Server-Sent Events) format.
func handleResponsesStream(
	ctx handlerctx.HandlerContext,
	w http.ResponseWriter,
	reqCtx context.Context,
	bCtx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostResponsesRequest,
) {
	log := ctx.Logger()
	log.Debug("executing bifrost stream responses request", "model", req.Model)

	budget := llmgateway.NewRetryBudget(ctx.Config().LLMGateway.Retry)
	ch, err := openResponsesStream(reqCtx, ctx, budget, bCtx, req)
	if err != nil {
		log.Error("bifrost stream responses request failed",
			"message", err.Error(),
			"model", req.Model, "provider", req.Provider)
		handlerctx.WriteErrorResponse(w, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: err.Error(),
			Code:    "upstream_error",
			Status:  http.StatusBadGateway,
		})
		return
	}

	// Set SSE response headers after the first successful open.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Error("response writer does not support flushing for SSE")
		return
	}

	chunkCount := 0
	for {
		ended := true
		for chunk := range ch {
			if chunk == nil {
				continue
			}
			if chunk.BifrostError != nil {
				msg := bifrostErrorMessage(chunk.BifrostError, "upstream stream error")
				if budget.RetryLeadingChunk(reqCtx, log, msg, chunkCount > 0) {
					llmgateway.DiscardStream(ch)
					var openErr error
					ch, openErr = openResponsesStream(reqCtx, ctx, budget, bCtx, req)
					if openErr != nil {
						fmt.Fprintf(w, "event: error\ndata: {\"message\":%q}\n\n", openErr.Error())
						flusher.Flush()
						return
					}
					ended = false
					break
				}
				errJSON, _ := json.Marshal(chunk.BifrostError)
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
				flusher.Flush()
				log.Debug("bifrost stream error chunk sent", "model", req.Model)
				return
			}
			if chunk.BifrostResponsesStreamResponse != nil {
				data, err := json.Marshal(chunk.BifrostResponsesStreamResponse)
				if err != nil {
					log.Error("failed to marshal stream chunk", "error", err)
					continue
				}
				eventType := string(chunk.BifrostResponsesStreamResponse.Type)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
				flusher.Flush()
				chunkCount++
			}
		}
		if ended {
			break
		}
	}

	log.Debug("bifrost stream completed", "model", req.Model, "chunks", chunkCount)
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
