package llmgateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandleAnthropicMessages_ToolCallFallback(t *testing.T) {
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]string{
				{"type": "text", "text": "Using search tool:\n<tool_call>{\"name\": \"google_search\", \"arguments\": {\"query\": \"golang\"}}</tool_call>"},
			},
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
		})
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["anthropic"]
	providerBaseURLs["anthropic"] = mockUpstream.URL
	defer func() { providerBaseURLs["anthropic"] = origURL }()

	cfg := &config.AppConfig{}

	profiles := &config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				Keys: []config.KeyConfig{
					{
						Name:  "primary",
						Value: "sk-ant-test-key",
						Models: []config.ModelConfig{
							{
								Name: "claude-sonnet-4-20250514",
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
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	content := response["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}

	block := content[0].(map[string]any)
	if block["type"] != "tool_use" {
		t.Errorf("expected type 'tool_use', got %v", block["type"])
	}
	if block["name"] != "google_search" {
		t.Errorf("expected name 'google_search', got %v", block["name"])
	}
	if response["stop_reason"] != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got %v", response["stop_reason"])
	}
}

func TestHandleAnthropicMessages_CrossProviderOpenAI(t *testing.T) {
	// Mock upstream OpenAI server that returns a Chat Completions response.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request was converted to OpenAI format
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header for OpenAI")
		}

		// Verify request body is in OpenAI format
		var oaiReq OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&oaiReq); err != nil {
			t.Fatalf("failed to decode OpenAI request: %v", err)
		}
		if oaiReq.Model != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", oaiReq.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test123",
			"choices": []map[string]any{
				{
					"message":       map[string]string{"role": "assistant", "content": "Hello from GPT!"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer mockUpstream.Close()

	// Override OpenAI base URL to point to mock
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	proxy := newTestProxyWithDriver(t)

	// Send Anthropic-format request with an OpenAI model
	body := map[string]any{
		"model":      "gpt-4o",
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
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	// Verify response is in Anthropic format
	var resp AnthropicResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse Anthropic response: %v", err)
	}
	if resp.Type != "message" {
		t.Errorf("type = %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("role = %q, want assistant", resp.Role)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello from GPT!" {
		t.Errorf("content unexpected: %+v", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("input_tokens = %d, want 10", resp.Usage.InputTokens)
	}
}

func TestHandleAnthropicMessages_UnsupportedProvider(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := &config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"custom_provider": {
				Keys: []config.KeyConfig{
					{
						Name:  "default",
						Value: "key-123",
						Models: []config.ModelConfig{
							{Name: "custom-model"},
						},
					},
				},
			},
		},
	}
	driver, _ := NewBifrostDriver(cfg, profiles, nil, nil)
	proxy := driver.proxy

	body := map[string]any{
		"model":      "custom-model",
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

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestHandleAnthropicMessages_CrossProviderOpenAI_Streaming(t *testing.T) {
	sseResponse := "data: {\"id\":\"chatcmpl-s1\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-s1\",\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-s1\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseResponse))
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	proxy := newTestProxyWithDriver(t)

	body := map[string]any{
		"model":      "gpt-4o",
		"max_tokens": 100,
		"stream":     true,
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
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	// Verify response is SSE format with Anthropic events
	output := rr.Body.String()
	if !strings.Contains(output, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Error("missing content_block_delta event")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("missing message_stop event")
	}
	if !strings.Contains(output, `"text":"Hi"`) {
		t.Error("missing text content 'Hi'")
	}
}
