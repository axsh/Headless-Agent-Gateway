package llmgateway

import (
	"encoding/json"
	"testing"
)

func TestConvertAnthropicRequestToOpenAI(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result []byte)
	}{
		{
			name: "basic text message",
			input: `{
				"model": "gpt-4o",
				"max_tokens": 1024,
				"messages": [
					{"role": "user", "content": "Hello"}
				]
			}`,
			check: func(t *testing.T, result []byte) {
				var req OpenAIRequest
				if err := json.Unmarshal(result, &req); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if req.Model != "gpt-4o" {
					t.Errorf("model = %q, want %q", req.Model, "gpt-4o")
				}
				if len(req.Messages) != 1 {
					t.Fatalf("messages count = %d, want 1", len(req.Messages))
				}
				if req.Messages[0].Role != "user" {
					t.Errorf("role = %q, want %q", req.Messages[0].Role, "user")
				}
				if req.Messages[0].Content != "Hello" {
					t.Errorf("content = %q, want %q", req.Messages[0].Content, "Hello")
				}
				if req.MaxTokens == nil || *req.MaxTokens != 1024 {
					t.Errorf("max_tokens unexpected")
				}
			},
		},
		{
			name: "system message insertion",
			input: `{
				"model": "gpt-4o",
				"max_tokens": 1024,
				"system": "You are helpful.",
				"messages": [
					{"role": "user", "content": "Hi"}
				]
			}`,
			check: func(t *testing.T, result []byte) {
				var req OpenAIRequest
				if err := json.Unmarshal(result, &req); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if len(req.Messages) != 2 {
					t.Fatalf("messages count = %d, want 2", len(req.Messages))
				}
				if req.Messages[0].Role != "system" {
					t.Errorf("first message role = %q, want %q", req.Messages[0].Role, "system")
				}
				if req.Messages[0].Content != "You are helpful." {
					t.Errorf("system content = %q, want %q", req.Messages[0].Content, "You are helpful.")
				}
				if req.Messages[1].Role != "user" {
					t.Errorf("second message role = %q, want %q", req.Messages[1].Role, "user")
				}
			},
		},
		{
			name: "content blocks array",
			input: `{
				"model": "gpt-4o",
				"max_tokens": 1024,
				"messages": [
					{"role": "user", "content": [
						{"type": "text", "text": "Part 1. "},
						{"type": "text", "text": "Part 2."}
					]}
				]
			}`,
			check: func(t *testing.T, result []byte) {
				var req OpenAIRequest
				if err := json.Unmarshal(result, &req); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if req.Messages[0].Content != "Part 1. Part 2." {
					t.Errorf("content = %q, want %q", req.Messages[0].Content, "Part 1. Part 2.")
				}
			},
		},
		{
			name: "temperature and stream passthrough",
			input: `{
				"model": "gpt-4o",
				"max_tokens": 512,
				"temperature": 0.7,
				"stream": false,
				"messages": [
					{"role": "user", "content": "Test"}
				]
			}`,
			check: func(t *testing.T, result []byte) {
				var req OpenAIRequest
				if err := json.Unmarshal(result, &req); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if req.Temperature == nil || *req.Temperature != 0.7 {
					t.Errorf("temperature unexpected")
				}
				if req.Stream == nil || *req.Stream != false {
					t.Errorf("stream unexpected")
				}
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertAnthropicRequestToOpenAI([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestConvertOpenAIResponseToAnthropic(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		model   string
		wantErr bool
		check   func(t *testing.T, result []byte)
	}{
		{
			name: "basic text response",
			input: `{
				"id": "chatcmpl-abc123",
				"choices": [{
					"message": {"role": "assistant", "content": "Hello world"},
					"finish_reason": "stop"
				}],
				"usage": {"prompt_tokens": 10, "completion_tokens": 5}
			}`,
			model: "gpt-4o",
			check: func(t *testing.T, result []byte) {
				var resp AnthropicResponse
				if err := json.Unmarshal(result, &resp); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if resp.ID != "chatcmpl-abc123" {
					t.Errorf("id = %q, want %q", resp.ID, "chatcmpl-abc123")
				}
				if resp.Type != "message" {
					t.Errorf("type = %q, want %q", resp.Type, "message")
				}
				if resp.Role != "assistant" {
					t.Errorf("role = %q, want %q", resp.Role, "assistant")
				}
				if resp.Model != "gpt-4o" {
					t.Errorf("model = %q, want %q", resp.Model, "gpt-4o")
				}
				if len(resp.Content) != 1 {
					t.Fatalf("content count = %d, want 1", len(resp.Content))
				}
				if resp.Content[0].Type != "text" {
					t.Errorf("content type = %q, want %q", resp.Content[0].Type, "text")
				}
				if resp.Content[0].Text != "Hello world" {
					t.Errorf("content text = %q, want %q", resp.Content[0].Text, "Hello world")
				}
				if resp.StopReason != "end_turn" {
					t.Errorf("stop_reason = %q, want %q", resp.StopReason, "end_turn")
				}
				if resp.Usage.InputTokens != 10 {
					t.Errorf("input_tokens = %d, want 10", resp.Usage.InputTokens)
				}
				if resp.Usage.OutputTokens != 5 {
					t.Errorf("output_tokens = %d, want 5", resp.Usage.OutputTokens)
				}
			},
		},
		{
			name: "finish_reason length maps to max_tokens",
			input: `{
				"id": "chatcmpl-xyz",
				"choices": [{
					"message": {"role": "assistant", "content": "truncated"},
					"finish_reason": "length"
				}],
				"usage": {"prompt_tokens": 5, "completion_tokens": 100}
			}`,
			model: "gpt-4o",
			check: func(t *testing.T, result []byte) {
				var resp AnthropicResponse
				if err := json.Unmarshal(result, &resp); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if resp.StopReason != "max_tokens" {
					t.Errorf("stop_reason = %q, want %q", resp.StopReason, "max_tokens")
				}
			},
		},
		{
			name: "empty choices",
			input: `{
				"id": "chatcmpl-empty",
				"choices": [],
				"usage": {"prompt_tokens": 0, "completion_tokens": 0}
			}`,
			model: "gpt-4o",
			check: func(t *testing.T, result []byte) {
				var resp AnthropicResponse
				if err := json.Unmarshal(result, &resp); err != nil {
					t.Fatalf("unmarshal result: %v", err)
				}
				if len(resp.Content) != 0 {
					t.Errorf("content count = %d, want 0", len(resp.Content))
				}
			},
		},
		{
			name:    "invalid json",
			input:   `{bad`,
			model:   "gpt-4o",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ConvertOpenAIResponseToAnthropic([]byte(tt.input), tt.model)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestConvertAnthropicRequestToOpenAI_SystemAsArray(t *testing.T) {
	input := `{
		"model": "gpt-4o",
		"max_tokens": 1024,
		"system": [
			{"type": "text", "text": "Rule 1. "},
			{"type": "text", "text": "Rule 2."}
		],
		"messages": [
			{"role": "user", "content": "Hi"}
		]
	}`

	result, err := ConvertAnthropicRequestToOpenAI([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req OpenAIRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("first role = %q, want system", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "Rule 1. Rule 2." {
		t.Errorf("system content = %q, want %q", req.Messages[0].Content, "Rule 1. Rule 2.")
	}
}
