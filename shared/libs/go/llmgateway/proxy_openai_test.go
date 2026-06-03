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

func TestHandleOpenAIChatCompletions_KnownModel_ForwardsToUpstream(t *testing.T) {
	// Use a mock upstream server to verify the request is forwarded.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "mock response"}},
			},
		})
	}))
	defer mockUpstream.Close()

	// Temporarily override the base URL
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

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

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}
