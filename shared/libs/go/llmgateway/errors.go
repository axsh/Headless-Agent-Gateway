package llmgateway

import (
	"net/http"

	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
)

// GatewayError is an alias for handlerctx.GatewayError for backward compatibility.
type GatewayError = handlerctx.GatewayError

// Pre-defined gateway errors.
var (
	ErrModelNotFound = &GatewayError{Type: "invalid_request_error", Message: "model not found", Code: "model_not_found", Status: 404}
	ErrProviderError = &GatewayError{Type: "api_error", Message: "provider error", Code: "provider_error", Status: 502}
	ErrInternalError = &GatewayError{Type: "api_error", Message: "internal server error", Code: "internal_error", Status: 500}
)

// WriteErrorResponse writes a JSON error response to w.
func WriteErrorResponse(w http.ResponseWriter, err *GatewayError) {
	handlerctx.WriteErrorResponse(w, err)
}
