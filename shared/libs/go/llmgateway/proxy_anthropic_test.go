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

func TestHandleAnthropicMessages_ResponsesMode_NonStream(t *testing.T) {
	// Mock upstream that simulates OpenAI Responses API.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request goes to /v1/responses
		if r.URL.Path != "/v1/responses" {
			t.Errorf("expected path /v1/responses, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header for OpenAI")
		}

		// Verify request body is in Responses API format
		var respReq ResponsesRequest
		if err := json.NewDecoder(r.Body).Decode(&respReq); err != nil {
			t.Fatalf("failed to decode Responses request: %v", err)
		}
		if respReq.Model != "gpt-5.3-codex" {
			t.Errorf("model = %q, want gpt-5.3-codex", respReq.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":     "resp_test123",
			"status": "completed",
			"output": []map[string]any{
				{
					"type": "message",
					"content": []map[string]string{
						{"type": "output_text", "text": "Hello from Codex!"},
					},
				},
			},
			"usage": map[string]int{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15},
		})
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	proxy := newTestProxyWithDriver(t)

	body := map[string]any{
		"model":      "gpt-5.3-codex",
		"max_tokens": 1024,
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
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello from Codex!" {
		t.Errorf("content unexpected: %+v", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", resp.StopReason)
	}
}

func TestHandleAnthropicMessages_ResponsesMode_Stream(t *testing.T) {
	sseResponse := "event: response.created\n" +
		`data: {"type":"response.created","response":{"id":"resp_s1","status":"in_progress"}}` + "\n\n" +
		"event: response.output_item.added\n" +
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","content":[]}}` + "\n\n" +
		"event: response.content_part.added\n" +
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}` + "\n\n" +
		"event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hi from Codex"}` + "\n\n" +
		"event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_s1","status":"completed"}}` + "\n\n"

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("expected path /v1/responses, got %s", r.URL.Path)
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
		"model":      "gpt-5.3-codex",
		"max_tokens": 1024,
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
	if !strings.Contains(output, `"text":"Hi from Codex"`) {
		t.Errorf("missing text content; got:\n%s", output)
	}
}

func TestHandleAnthropicMessages_ChatMode_Unchanged(t *testing.T) {
	// Verify that mode="" (default) still routes to /v1/chat/completions.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-unchanged",
			"choices": []map[string]any{
				{
					"message":       map[string]string{"role": "assistant", "content": "Hi from GPT"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 3},
		})
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = mockUpstream.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	proxy := newTestProxyWithDriver(t)

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
}

func TestHandleAnthropicMessages_CrossProviderGemini(t *testing.T) {
	// Mock upstream Gemini server
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-3.5-flash:generateContent" {
			t.Errorf("expected path /v1beta/models/gemini-3.5-flash:generateContent, got %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "AIzaSy-test-key" {
			t.Errorf("expected x-goog-api-key 'AIzaSy-test-key', got %s", r.Header.Get("x-goog-api-key"))
		}
		if r.URL.Query().Get("key") != "AIzaSy-test-key" {
			t.Errorf("expected key parameter 'AIzaSy-test-key', got %s", r.URL.Query().Get("key"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"role": "model",
						"parts": []map[string]any{
							{"text": "Hello from Gemini!"},
						},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]int{
				"promptTokenCount":     10,
				"candidatesTokenCount": 5,
				"totalTokenCount":      15,
			},
		})
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["google"]
	providerBaseURLs["google"] = mockUpstream.URL
	defer func() { providerBaseURLs["google"] = origURL }()

	proxy := newTestProxyWithDriver(t)

	body := map[string]any{
		"model":      "gemini-3.5-flash",
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
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello from Gemini!" {
		t.Errorf("content unexpected: %+v", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", resp.StopReason)
	}
}

func TestHandleAnthropicMessages_CrossProviderGemini_Streaming(t *testing.T) {
	sseResponse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" from Gemini!\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":5,\"totalTokenCount\":15}}\n\n"

	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-3.5-flash:streamGenerateContent" {
			t.Errorf("expected path /v1beta/models/gemini-3.5-flash:streamGenerateContent, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("expected alt=sse, got %s", r.URL.Query().Get("alt"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseResponse))
	}))
	defer mockUpstream.Close()

	origURL := providerBaseURLs["google"]
	providerBaseURLs["google"] = mockUpstream.URL
	defer func() { providerBaseURLs["google"] = origURL }()

	proxy := newTestProxyWithDriver(t)

	body := map[string]any{
		"model":      "gemini-3.5-flash",
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
	if !strings.Contains(output, `"text":"Hello"`) {
		t.Error("missing 'Hello'")
	}
	if !strings.Contains(output, `"text":" from Gemini!"`) {
		t.Error("missing ' from Gemini!'")
	}
}

