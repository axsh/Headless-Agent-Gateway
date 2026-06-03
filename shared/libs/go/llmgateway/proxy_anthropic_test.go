package llmgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/hag/config"
)

func newTestProxyWithDriver(t *testing.T) *ProxyServer {
	t.Helper()
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
	if err != nil {
		t.Fatalf("NewBifrostDriver: %v", err)
	}
	return driver.proxy
}

func TestHandleAnthropicMessages_UnknownModel(t *testing.T) {
	proxy := newTestProxyWithDriver(t)

	body := map[string]any{
		"model":      "nonexistent-model",
		"max_tokens": 100,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleAnthropicMessages(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}
	if errObj["code"] != "model_not_found" {
		t.Errorf("error code = %v, want model_not_found", errObj["code"])
	}
}

func TestHandleAnthropicMessages_EmptyBody(t *testing.T) {
	proxy := newTestProxyWithDriver(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleAnthropicMessages(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (empty model should be not found)", rr.Code, http.StatusNotFound)
	}
}

func TestHandleAnthropicMessages_InvalidJSON(t *testing.T) {
	proxy := newTestProxyWithDriver(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleAnthropicMessages(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleAnthropicMessages_KnownModel_ForwardsToUpstream(t *testing.T) {
	// When a known model is routed, the handler should forward to upstream.
	// Use a mock upstream server to verify the request reaches it.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("x-api-key") == "" {
			t.Error("expected x-api-key header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "mock response"},
			},
		})
	}))
	defer mockUpstream.Close()

	// Temporarily override the base URL
	origURL := providerBaseURLs["anthropic"]
	providerBaseURLs["anthropic"] = mockUpstream.URL
	defer func() { providerBaseURLs["anthropic"] = origURL }()

	proxy := newTestProxyWithDriver(t)

	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 100,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleAnthropicMessages(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}
