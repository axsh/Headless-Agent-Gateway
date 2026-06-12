package llmgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)




func TestIsStreamRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"stream true", `{"model":"gpt-4o","stream":true}`, true},
		{"stream false", `{"model":"gpt-4o","stream":false}`, false},
		{"no stream field", `{"model":"gpt-4o"}`, false},
		{"invalid json", `bad json`, false},
		{"empty body", ``, false},
		{"stream null", `{"model":"gpt-4o","stream":null}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isStreamRequest([]byte(tc.body))
			if got != tc.want {
				t.Errorf("isStreamRequest(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestToBifrostProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantStr  string
	}{
		{"openai", "openai", "openai"},
		{"anthropic", "anthropic", "anthropic"},
		{"google maps to gemini", "google", "gemini"},
		{"gemini direct", "gemini", "gemini"},
		{"unknown passthrough", "some-custom-provider", "some-custom-provider"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ToBifrostProvider(tc.provider)
			if string(got) != tc.wantStr {
				t.Errorf("ToBifrostProvider(%q) = %q, want %q", tc.provider, got, tc.wantStr)
			}
		})
	}
}

func TestOpenAIHandler_MaxBodySize(t *testing.T) {
	proxy := newTestProxyWithDriver(t)
	proxy.cfg.LLMGateway.MaxRequestBodyBytes = 10

	// 10 bytes body
	bodyBytes := []byte(`{"a":"b"}`) // 9 bytes
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleOpenAIResponses(rr, req)
	if rr.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("expected small request to pass, got status %d", rr.Code)
	}

	// 11 bytes body
	bodyBytesLarge := []byte(`{"abc":"def"}`) // 13 bytes
	reqLarge := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(bodyBytesLarge))
	reqLarge.Header.Set("Content-Type", "application/json")
	rrLarge := httptest.NewRecorder()

	proxy.handleOpenAIResponses(rrLarge, reqLarge)
	if rrLarge.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d (Request Entity Too Large)", rrLarge.Code, http.StatusRequestEntityTooLarge)
	}

	var resp map[string]any
	if err := json.Unmarshal(rrLarge.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}
	if errObj["code"] != "request_too_large" {
		t.Errorf("error code = %v, want request_too_large", errObj["code"])
	}
}

