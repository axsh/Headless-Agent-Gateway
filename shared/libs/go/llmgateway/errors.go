package llmgateway

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// GatewayError represents a structured error response compatible with
// OpenAI/Anthropic error formats.
type GatewayError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"-"` // HTTP status code (not serialized)
}

// Error implements the error interface.
func (e *GatewayError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Pre-defined gateway errors.
var (
	ErrModelNotFound = &GatewayError{Type: "invalid_request_error", Message: "model not found", Code: "model_not_found", Status: 404}
	ErrProviderError = &GatewayError{Type: "api_error", Message: "provider error", Code: "provider_error", Status: 502}
	ErrInternalError = &GatewayError{Type: "api_error", Message: "internal server error", Code: "internal_error", Status: 500}
)

// errorResponse is the JSON envelope for error responses.
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// WriteErrorResponse writes a JSON error response to w.
func WriteErrorResponse(w http.ResponseWriter, err *GatewayError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Status)

	resp := errorResponse{
		Error: errorBody{
			Type:    err.Type,
			Message: err.Message,
			Code:    err.Code,
		},
	}
	json.NewEncoder(w).Encode(resp)
}
