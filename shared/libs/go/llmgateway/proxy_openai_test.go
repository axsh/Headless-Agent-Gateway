package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/config"
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

func TestHandleOpenAIChatCompletions_Stream(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if ok {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
			flusher.Flush()
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, _ := NewBifrostDriver(cfg, profiles, nil, nil)
	proxy := driver.proxy

	body := map[string]any{
		"model":  "gpt-4o",
		"stream": true,
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
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	if rr.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", rr.Header().Get("Content-Type"), "text/event-stream")
	}

	bodyStr := rr.Body.String()
	if !strings.Contains(bodyStr, "hello") || !strings.Contains(bodyStr, "[DONE]") {
		t.Errorf("body = %q, expected streaming data chunks", bodyStr)
	}
}

func TestHandleOpenAIChatCompletions_ToolCallFallback(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "Using weather tool:\n<tool_call>{\"name\": \"get_weather\", \"arguments\": {\"location\": \"Tokyo\"}}</tool_call>",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &config.AppConfig{}

	profiles := &config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {
				Keys: []config.KeyConfig{
					{
						Name:  "default",
						Value: "sk-openai-test-key",
						Models: []config.ModelConfig{
							{
								Name: "gpt-4o",
								Behavior: &config.ModelBehavior{
									ToolCallFallback: true,
								},
							},
						},
					},
				},
			},
		},
	}
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
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	choices := response["choices"].([]any)
	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)

	toolCalls, exists := message["tool_calls"]
	if !exists || toolCalls == nil {
		t.Fatalf("expected tool_calls to be populated")
	}

	tcs := toolCalls.([]any)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tcs))
	}

	tc := tcs[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %v", fn["name"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %v", choice["finish_reason"])
	}
}

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
			got := toBifrostProvider(tc.provider)
			if string(got) != tc.wantStr {
				t.Errorf("toBifrostProvider(%q) = %q, want %q", tc.provider, got, tc.wantStr)
			}
		})
	}
}

