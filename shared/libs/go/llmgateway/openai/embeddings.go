package openai

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

func init() {
	handlerctx.RegisterHandler("POST /v1/embeddings", HandleEmbeddings)
}

// openaiEmbeddingRequest is the OpenAI-compatible request body.
type openaiEmbeddingRequest struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"` // string or []string
	EncodingFormat *string         `json:"encoding_format,omitempty"`
	Dimensions     *int            `json:"dimensions,omitempty"`
}

// embeddingInvokerOverride, when non-nil, replaces Bifrost EmbeddingRequest (tests).
var embeddingInvokerOverride func(
	ctx handlerctx.HandlerContext,
	bCtx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostEmbeddingRequest,
) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError)

func invokeEmbedding(
	ctx handlerctx.HandlerContext,
	bCtx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostEmbeddingRequest,
) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError) {
	if embeddingInvokerOverride != nil {
		return embeddingInvokerOverride(ctx, bCtx, req)
	}
	sdk := ctx.BifrostSDK()
	if sdk == nil {
		return nil, &bifrostSchemas.BifrostError{
			Error: &bifrostSchemas.ErrorField{Message: "Bifrost SDK not initialized"},
		}
	}
	return sdk.EmbeddingRequest(bCtx, req)
}

// HandleEmbeddings returns an http.HandlerFunc for POST /v1/embeddings.
func HandleEmbeddings(ctx handlerctx.HandlerContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleEmbeddings(ctx, w, r)
	}
}

func handleEmbeddings(ctx handlerctx.HandlerContext, w http.ResponseWriter, r *http.Request) {
	cfg := ctx.Config()
	log := ctx.Logger()
	modelName := ""

	if maxBody := cfg.LLMGateway.MaxRequestBodyBytes; maxBody > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
				Type:    "invalid_request_error",
				Message: "request body too large",
				Code:    "request_too_large",
				Status:  http.StatusRequestEntityTooLarge,
			})
			return
		}
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "failed to read request body",
			Code:    "request_read_error",
			Status:  http.StatusBadRequest,
		})
		return
	}
	defer r.Body.Close()

	var req openaiEmbeddingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "invalid JSON in request body",
			Code:    "invalid_json",
			Status:  http.StatusBadRequest,
		})
		return
	}

	modelName = req.Model
	if req.Model == "" {
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "model is required",
			Code:    "missing_model",
			Status:  http.StatusBadRequest,
		})
		return
	}

	input, err := parseEmbeddingInput(req.Input)
	if err != nil {
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: err.Error(),
			Code:    "invalid_input",
			Status:  http.StatusBadRequest,
		})
		return
	}

	log.Debug("openai embeddings request received", "method", r.Method, "path", r.URL.Path, "model", req.Model)

	router := ctx.Router()
	if router == nil {
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
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
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "not_found_error",
			Message: "model not found: " + req.Model,
			Code:    "model_not_found",
			Status:  http.StatusNotFound,
		})
		return
	}

	if !config.IsEmbeddingMode(routed.Mode) {
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "invalid_request_error",
			Message: "model is not an embedding model: " + req.Model,
			Code:    "invalid_model_mode",
			Status:  http.StatusBadRequest,
		})
		return
	}

	log.Debug("embeddings request routed", "model", routed.Model, "provider", routed.Provider, "mode", routed.Mode)

	if ctx.BifrostSDK() == nil && embeddingInvokerOverride == nil {
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: "Bifrost SDK not initialized",
			Code:    "not_configured",
			Status:  http.StatusServiceUnavailable,
		})
		return
	}

	apiKey := routed.KeyValue
	if vault.IsVaultRef(apiKey) && ctx.Vault() != nil {
		resolved, resolveErr := ctx.Vault().Resolve(apiKey)
		if resolveErr != nil {
			writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
				Type:    "api_error",
				Message: "failed to resolve API key from vault",
				Code:    "vault_error",
				Status:  http.StatusInternalServerError,
			})
			return
		}
		apiKey = resolved
	}

	log.Info("openai embeddings request via bifrost",
		"model", routed.Model,
		"provider", routed.Provider,
		"key", ctx.MaskSecret(apiKey),
	)

	providerKey := ctx.ToBifrostProvider(routed.Provider)
	bifrostReq := &bifrostSchemas.BifrostEmbeddingRequest{
		Provider: providerKey,
		Model:    routed.Model,
		Input:    input,
	}
	if req.EncodingFormat != nil || req.Dimensions != nil {
		bifrostReq.Params = &bifrostSchemas.EmbeddingParameters{
			EncodingFormat: req.EncodingFormat,
			Dimensions:     req.Dimensions,
		}
	}

	bifrostCtx := bifrostSchemas.NewBifrostContext(r.Context(), bifrostSchemas.NoDeadline)
	resp, bifrostErr := invokeEmbedding(ctx, bifrostCtx, bifrostReq)
	if bifrostErr != nil {
		status := http.StatusBadGateway
		if bifrostErr.StatusCode != nil {
			status = *bifrostErr.StatusCode
		}
		msg := "upstream request failed"
		if bifrostErr.Error != nil {
			msg = bifrostErr.Error.Message
		}
		log.Error("bifrost embeddings request failed",
			"status", status, "message", msg,
			"model", routed.Model, "provider", routed.Provider)
		writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
			Type:    "api_error",
			Message: msg,
			Code:    "upstream_error",
			Status:  status,
		})
		return
	}

	log.Debug("bifrost embeddings request succeeded", "model", routed.Model, "provider", routed.Provider)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func parseEmbeddingInput(raw json.RawMessage) (*bifrostSchemas.EmbeddingInput, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("input is required")
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return nil, errors.New("input is required")
		}
		return &bifrostSchemas.EmbeddingInput{Text: &single}, nil
	}
	var batch []string
	if err := json.Unmarshal(raw, &batch); err == nil {
		if len(batch) == 0 {
			return nil, errors.New("input is required")
		}
		return &bifrostSchemas.EmbeddingInput{Texts: batch}, nil
	}
	return nil, errors.New("input must be a string or an array of strings")
}
