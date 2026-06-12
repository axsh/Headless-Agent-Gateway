package wayfinder

import (
	"context"
	"testing"
)

func TestBifrostClient_ImplementsLLMClient(t *testing.T) {
	var _ LLMClient = (*BifrostClient)(nil)
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
	if body["max_tokens"] != 4096 {
		t.Errorf("max_tokens = %v, want 4096", body["max_tokens"])
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
