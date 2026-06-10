package llmgateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractToolCallFromText_XML(t *testing.T) {
	text := "Here is my tool call:\n<tool_call>{\"name\": \"get_weather\", \"arguments\": {\"location\": \"Tokyo\"}}</tool_call>\nHope that helps!"
	call, ok := ExtractToolCallFromText(text)
	if !ok {
		t.Fatalf("expected tool call to be extracted")
	}
	if call.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", call.Name)
	}
	if loc, exists := call.Arguments["location"]; !exists || loc != "Tokyo" {
		t.Errorf("expected location 'Tokyo', got %v", loc)
	}
}

func TestExtractToolCallFromText_JSON(t *testing.T) {
	text := "Here is my tool call:\n{\n  \"name\": \"search\",\n  \"arguments\": {\"query\": \"golang\"}\n}\nHave a nice day!"
	call, ok := ExtractToolCallFromText(text)
	if !ok {
		t.Fatalf("expected tool call to be extracted")
	}
	if call.Name != "search" {
		t.Errorf("expected name 'search', got %q", call.Name)
	}
	if query, exists := call.Arguments["query"]; !exists || query != "golang" {
		t.Errorf("expected query 'golang', got %v", query)
	}
}

func TestExtractToolCallFromText_None(t *testing.T) {
	text := "This is just standard text without any tool calls."
	_, ok := ExtractToolCallFromText(text)
	if ok {
		t.Errorf("expected no tool call to be extracted")
	}
}

func TestTryFallbackOpenAIResponse(t *testing.T) {
	rawResponse := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Using weather tool:\n<tool_call>{\"name\": \"get_weather\", \"arguments\": {\"location\": \"Tokyo\"}}</tool_call>"
			},
			"finish_reason": "stop"
		}]
	}`

	rewritten, ok := TryFallbackOpenAIResponse([]byte(rawResponse))
	if !ok {
		t.Fatalf("expected response to be rewritten")
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(rewritten, &parsed); err != nil {
		t.Fatalf("failed to parse rewritten JSON: %v", err)
	}

	if len(parsed.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(parsed.Choices[0].Message.ToolCalls))
	}

	tc := parsed.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected name 'get_weather', got %q", tc.Function.Name)
	}
	if !strings.Contains(tc.Function.Arguments, "Tokyo") {
		t.Errorf("expected arguments to contain 'Tokyo', got %q", tc.Function.Arguments)
	}
	if parsed.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %q", parsed.Choices[0].FinishReason)
	}
	if parsed.Choices[0].Message.Content != "" {
		t.Errorf("expected content to be cleared, got %q", parsed.Choices[0].Message.Content)
	}
}

func TestTryFallbackAnthropicResponse(t *testing.T) {
	rawResponse := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{
			"type": "text",
			"text": "Using search tool:\n<tool_call>{\"name\": \"google_search\", \"arguments\": {\"query\": \"golang\"}}</tool_call>"
		}],
		"model": "claude-3-5-sonnet",
		"stop_reason": "end_turn"
	}`

	rewritten, ok := TryFallbackAnthropicResponse([]byte(rawResponse))
	if !ok {
		t.Fatalf("expected response to be rewritten")
	}

	var parsed struct {
		Content []map[string]any `json:"content"`
		StopReason string `json:"stop_reason"`
	}

	if err := json.Unmarshal(rewritten, &parsed); err != nil {
		t.Fatalf("failed to parse rewritten JSON: %v", err)
	}

	if len(parsed.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(parsed.Content))
	}

	block := parsed.Content[0]
	if block["type"] != "tool_use" {
		t.Errorf("expected type 'tool_use', got %q", block["type"])
	}
	if block["name"] != "google_search" {
		t.Errorf("expected name 'google_search', got %q", block["name"])
	}
	if parsed.StopReason != "tool_use" {
		t.Errorf("expected stop_reason 'tool_use', got %q", parsed.StopReason)
	}
}

// R8: Test ExtractFallbackFlag function.
func TestExtractFallbackFlag(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"with_fallback_true", "not-needed;fallback=true;sid=abc", true},
		{"with_fallback_false", "not-needed;fallback=false;sid=abc", false},
		{"no_fallback_part", "not-needed", false},
		{"empty_string", "", false},
		{"only_sid", "not-needed;sid=abc", false},
		{"bearer_prefix", "Bearer not-needed;fallback=true;sid=abc", true},
		{"bearer_fallback_false", "Bearer not-needed;fallback=false;sid=abc", false},
		{"spaces_around", "not-needed; fallback=true ; sid=abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFallbackFlag(tt.header)
			if got != tt.want {
				t.Errorf("ExtractFallbackFlag(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}
