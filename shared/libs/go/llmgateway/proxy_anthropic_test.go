package llmgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/config"
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

func TestAnthropicHandler_MaxBodySize(t *testing.T) {
	proxy := newTestProxyWithDriver(t)
	proxy.cfg.LLMGateway.MaxRequestBodyBytes = 10

	// 10 bytes body
	bodyBytes := []byte(`{"a":"b"}`) // 9 bytes
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleAnthropicMessages(rr, req)
	if rr.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("expected small request to pass, got status %d", rr.Code)
	}

	// 11 bytes body (limit is 10)
	bodyBytesLarge := []byte(`{"abc":"def"}`) // 13 bytes
	reqLarge := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyBytesLarge))
	reqLarge.Header.Set("Content-Type", "application/json")
	rrLarge := httptest.NewRecorder()

	proxy.handleAnthropicMessages(rrLarge, reqLarge)
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


