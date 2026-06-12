package llmgateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestGatewayError_Error(t *testing.T) {
	err := &GatewayError{
		Type:    "invalid_request_error",
		Message: "model not found",
		Code:    "model_not_found",
		Status:  404,
	}
	got := err.Error()
	want := "invalid_request_error: model not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestWriteErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		err        *GatewayError
		wantStatus int
		wantType   string
		wantMsg    string
		wantCode   string
	}{
		{
			name:       "model_not_found",
			err:        ErrModelNotFound,
			wantStatus: 404,
			wantType:   "invalid_request_error",
			wantMsg:    "model not found",
			wantCode:   "model_not_found",
		},
		{
			name:       "provider_error",
			err:        ErrProviderError,
			wantStatus: 502,
			wantType:   "api_error",
			wantMsg:    "provider error",
			wantCode:   "provider_error",
		},
		{
			name:       "internal_error",
			err:        ErrInternalError,
			wantStatus: 500,
			wantType:   "api_error",
			wantMsg:    "internal server error",
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteErrorResponse(w, tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var body struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
					Code    string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("json decode error: %v", err)
			}
			if body.Error.Type != tt.wantType {
				t.Errorf("error.type = %q, want %q", body.Error.Type, tt.wantType)
			}
			if body.Error.Message != tt.wantMsg {
				t.Errorf("error.message = %q, want %q", body.Error.Message, tt.wantMsg)
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error.code = %q, want %q", body.Error.Code, tt.wantCode)
			}
		})
	}
}
