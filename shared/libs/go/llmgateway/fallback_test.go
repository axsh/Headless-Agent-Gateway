package llmgateway

import (
	"encoding/json"
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
		Content    []map[string]any `json:"content"`
		StopReason string           `json:"stop_reason"`
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
