package llmgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/hag/config"
)

func TestHandleOpenAIChatCompletions_UnknownModel(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, _ := NewBifrostDriver(cfg, profiles, nil, nil)
	proxy := driver.proxy

	body := map[string]any{
		"model": "nonexistent-model",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleOpenAIChatCompletions(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandleOpenAIChatCompletions_InvalidJSON(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, _ := NewBifrostDriver(cfg, profiles, nil, nil)
	proxy := driver.proxy

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte("bad json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleOpenAIChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleOpenAIChatCompletions_KnownModel_NoBifrost(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, _ := NewBifrostDriver(cfg, profiles, nil, nil)
	proxy := driver.proxy

	body := map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	proxy.handleOpenAIChatCompletions(rr, req)

	// Without Bifrost SDK initialized, should return 503
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
