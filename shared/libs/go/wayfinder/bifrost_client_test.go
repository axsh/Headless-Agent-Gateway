package wayfinder

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBifrostClient_ImplementsLLMClient(t *testing.T) {
	var _ LLMClient = (*BifrostClient)(nil)
}

func TestBifrostClient_ImplementsStreamingLLMClient(t *testing.T) {
	var _ StreamingLLMClient = (*BifrostClient)(nil)
}

func TestBifrostClient_NewWithDefaults(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	if client.baseURL != "http://127.0.0.1:8080" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "http://127.0.0.1:8080")
	}
	if client.token != "test-token" {
		t.Errorf("token = %q, want %q", client.token, "test-token")
	}
}

func TestBifrostClient_BuildRequestBody(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}
	tools := []ToolDefinition{
		{Name: "read_file", Description: "Read a file", InputSchema: map[string]any{"type": "object"}},
	}

	body := client.buildRequestBody("test-model", messages, tools)
	if body["model"] != "test-model" {
		t.Errorf("model = %v, want %q", body["model"], "test-model")
	}
	if body["max_tokens"] != 16384 {
		t.Errorf("max_tokens = %v, want 16384", body["max_tokens"])
	}
	msgList, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages is not []map[string]any")
	}
	if len(msgList) != 1 {
		t.Errorf("len(messages) = %d, want 1", len(msgList))
	}
	toolList, ok := body["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools is not []map[string]any")
	}
	if len(toolList) != 1 {
		t.Errorf("len(tools) = %d, want 1", len(toolList))
	}
}

func TestBifrostClient_ParseToolUseResponse(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")

	// Simulate an Anthropic response with tool_use blocks.
	respBody := map[string]any{
		"type": "message",
		"content": []any{
			map[string]any{
				"type": "tool_use",
				"id":   "call_123",
				"name": "read_file",
				"input": map[string]any{
					"path": "test.txt",
				},
			},
		},
	}

	resp, err := client.parseResponse(respBody)
	if err != nil {
		t.Fatalf("parseResponse failed: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", resp.ToolCalls[0].Name, "read_file")
	}
	if resp.ToolCalls[0].ID != "call_123" {
		t.Errorf("ToolCalls[0].ID = %q, want %q", resp.ToolCalls[0].ID, "call_123")
	}
}

func TestBifrostClient_ParseTextResponse(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")

	respBody := map[string]any{
		"type": "message",
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "Hello, world!",
			},
		},
	}

	resp, err := client.parseResponse(respBody)
	if err != nil {
		t.Fatalf("parseResponse failed: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world!")
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("len(ToolCalls) = %d, want 0", len(resp.ToolCalls))
	}
}

// Integration test (requires running tern server) - skip in unit tests.
func TestBifrostClient_GenerateMessage_Integration(t *testing.T) {
	t.Skip("integration test: requires running tern server")

	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	messages := []ChatMessage{
		{Role: "user", Content: "Say hello"},
	}
	resp, err := client.GenerateMessage(context.Background(), "claude-sonnet-4-20250514", messages, nil)
	if err != nil {
		t.Fatalf("GenerateMessage failed: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty response")
	}
}

// ---- Streaming Tests ----

func TestBifrostClient_BuildRequestBody_WithStream(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
	}

	body := client.buildStreamRequestBody("test-model", messages, nil)
	if body["stream"] != true {
		t.Errorf("stream = %v, want true", body["stream"])
	}
	if body["model"] != "test-model" {
		t.Errorf("model = %v, want %q", body["model"], "test-model")
	}
}

func TestBifrostClient_GenerateMessageStream_TextOnly(t *testing.T) {
	// Mock SSE server that returns text deltas.
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[]}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" World"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	client := NewBifrostClient(server.URL, "test-token")

	var deltas []string
	resp, err := client.GenerateMessageStream(
		context.Background(),
		"test-model",
		[]ChatMessage{{Role: "user", Content: "Hi"}},
		nil,
		func(delta string) { deltas = append(deltas, delta) },
	)
	if err != nil {
		t.Fatalf("GenerateMessageStream failed: %v", err)
	}

	// Verify deltas were received.
	if len(deltas) != 2 {
		t.Errorf("delta count = %d, want 2, got %v", len(deltas), deltas)
	}
	if len(deltas) >= 2 {
		if deltas[0] != "Hello" {
			t.Errorf("delta[0] = %q, want %q", deltas[0], "Hello")
		}
		if deltas[1] != " World" {
			t.Errorf("delta[1] = %q, want %q", deltas[1], " World")
		}
	}

	// Verify final response.
	if resp.Content != "Hello World" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello World")
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("len(ToolCalls) = %d, want 0", len(resp.ToolCalls))
	}
}

func TestBifrostClient_GenerateMessageStream_WithToolCalls(t *testing.T) {
	// Mock SSE server that returns text + tool_use.
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[]}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me read that."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_abc","name":"read_file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"test.txt\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse)
	}))
	defer server.Close()

	client := NewBifrostClient(server.URL, "test-token")

	var deltas []string
	resp, err := client.GenerateMessageStream(
		context.Background(),
		"test-model",
		[]ChatMessage{{Role: "user", Content: "Read a file"}},
		nil,
		func(delta string) { deltas = append(deltas, delta) },
	)
	if err != nil {
		t.Fatalf("GenerateMessageStream failed: %v", err)
	}

	// Verify text deltas.
	if len(deltas) != 1 {
		t.Errorf("text delta count = %d, want 1", len(deltas))
	}

	// Verify text content.
	if resp.Content != "Let me read that." {
		t.Errorf("Content = %q, want %q", resp.Content, "Let me read that.")
	}

	// Verify tool calls.
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCalls[0].ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", tc.Name, "read_file")
	}
	if path, ok := tc.Input["path"].(string); !ok || path != "test.txt" {
		t.Errorf("ToolCalls[0].Input[path] = %v, want %q", tc.Input["path"], "test.txt")
	}
}

func TestBifrostClient_GenerateMessageStream_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal server error"}`)
	}))
	defer server.Close()

	client := NewBifrostClient(server.URL, "test-token")

	_, err := client.GenerateMessageStream(
		context.Background(),
		"test-model",
		[]ChatMessage{{Role: "user", Content: "Hi"}},
		nil,
		func(delta string) {},
	)
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got %q", err.Error())
	}
}

// ---- Empty Content Sanitization Tests ----

func TestBuildRequestBody_EmptyToolContent(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "I'll use a tool.", ToolCalls: []ToolCall{
			{ID: "tc1", Name: "execute_command", Input: map[string]any{"command": "echo"}},
		}},
		{Role: "tool", Content: "", ToolCallID: "tc1"},
	}

	body := client.buildRequestBody("test-model", messages, nil)
	msgList := body["messages"].([]map[string]any)

	// Third message (index 2) is the tool result.
	toolMsg := msgList[2]
	contentBlocks, ok := toolMsg["content"].([]map[string]any)
	if !ok {
		t.Fatalf("tool message content is not []map[string]any: %T", toolMsg["content"])
	}
	if len(contentBlocks) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contentBlocks))
	}
	toolContent, _ := contentBlocks[0]["content"].(string)
	if toolContent != "(no output)" {
		t.Errorf("empty tool content should be sanitized to %q, got %q", "(no output)", toolContent)
	}
}

func TestBuildRequestBody_EmptyAssistantContent(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: ""},
	}

	body := client.buildRequestBody("test-model", messages, nil)
	msgList := body["messages"].([]map[string]any)

	// Second message (index 1) is the assistant with empty content.
	assistantContent, _ := msgList[1]["content"].(string)
	if assistantContent != "(empty)" {
		t.Errorf("empty assistant content should be sanitized to %q, got %q", "(empty)", assistantContent)
	}
}

func TestBuildRequestBody_NonEmptyToolContentUnchanged(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	messages := []ChatMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Using tool.", ToolCalls: []ToolCall{
			{ID: "tc1", Name: "read_file", Input: map[string]any{"path": "test.txt"}},
		}},
		{Role: "tool", Content: "file contents here", ToolCallID: "tc1"},
	}

	body := client.buildRequestBody("test-model", messages, nil)
	msgList := body["messages"].([]map[string]any)

	toolMsg := msgList[2]
	contentBlocks := toolMsg["content"].([]map[string]any)
	toolContent, _ := contentBlocks[0]["content"].(string)
	if toolContent != "file contents here" {
		t.Errorf("non-empty tool content should be unchanged, got %q", toolContent)
	}
}

// ---- StopReason Tests ----

func TestBifrostClient_ParseResponse_StopReason(t *testing.T) {
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")

	tests := []struct {
		name       string
		respBody   map[string]any
		wantReason string
	}{
		{
			name: "end_turn",
			respBody: map[string]any{
				"stop_reason": "end_turn",
				"content":     []any{map[string]any{"type": "text", "text": "hello"}},
			},
			wantReason: "end_turn",
		},
		{
			name: "max_tokens",
			respBody: map[string]any{
				"stop_reason": "max_tokens",
				"content":     []any{map[string]any{"type": "text", "text": "truncated"}},
			},
			wantReason: "max_tokens",
		},
		{
			name: "no stop_reason",
			respBody: map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "hi"}},
			},
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.parseResponse(tt.respBody)
			if err != nil {
				t.Fatalf("parseResponse failed: %v", err)
			}
			if resp.StopReason != tt.wantReason {
				t.Errorf("StopReason = %q, want %q", resp.StopReason, tt.wantReason)
			}
		})
	}
}

func TestBifrostClient_ParseSSEStream_StopReason(t *testing.T) {
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[]}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}

event: message_stop
data: {"type":"message_stop"}

`
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	resp, err := client.parseSSEStream(strings.NewReader(sseResponse), nil)
	if err != nil {
		t.Fatalf("parseSSEStream failed: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
	if resp.Content != "Hi" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hi")
	}
}

func TestBifrostClient_ParseSSEStream_StopReason_MaxTokens(t *testing.T) {
	sseResponse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[]}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial output"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":4096}}

event: message_stop
data: {"type":"message_stop"}

`
	client := NewBifrostClient("http://127.0.0.1:8080", "test-token")
	resp, err := client.parseSSEStream(strings.NewReader(sseResponse), nil)
	if err != nil {
		t.Fatalf("parseSSEStream failed: %v", err)
	}
	if resp.StopReason != "max_tokens" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "max_tokens")
	}
}

