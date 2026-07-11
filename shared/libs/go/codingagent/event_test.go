package codingagent_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestStreamEventJSONMarshal(t *testing.T) {
	tests := []struct {
		name   string
		event  codingagent.StreamEvent
		checks func(t *testing.T, data map[string]any)
	}{
		{
			name: "EventText contains only type and content",
			event: codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: "hello world",
			},
			checks: func(t *testing.T, data map[string]any) {
				if data["type"] != "text" {
					t.Errorf("type = %v, want text", data["type"])
				}
				if data["content"] != "hello world" {
					t.Errorf("content = %v, want hello world", data["content"])
				}
				if _, ok := data["tool_name"]; ok {
					t.Error("tool_name should be omitted")
				}
			},
		},
		{
			name: "EventToolUse contains tool_name and tool_input",
			event: codingagent.StreamEvent{
				Type:      codingagent.EventToolUse,
				ToolName:  "Write",
				ToolInput: map[string]interface{}{"path": "main.go"},
			},
			checks: func(t *testing.T, data map[string]any) {
				if data["type"] != "tool_use" {
					t.Errorf("type = %v, want tool_use", data["type"])
				}
				if data["tool_name"] != "Write" {
					t.Errorf("tool_name = %v, want Write", data["tool_name"])
				}
				input, ok := data["tool_input"].(map[string]any)
				if !ok {
					t.Fatal("tool_input should be a map")
				}
				if input["path"] != "main.go" {
					t.Errorf("tool_input.path = %v, want main.go", input["path"])
				}
			},
		},
		{
			name: "EventError excludes Error field from JSON",
			event: codingagent.StreamEvent{
				Type:  codingagent.EventError,
				Error: errors.New("something failed"),
			},
			checks: func(t *testing.T, data map[string]any) {
				if data["type"] != "error" {
					t.Errorf("type = %v, want error", data["type"])
				}
				if _, ok := data["error"]; ok {
					t.Error("error field should be excluded (json:\"-\")")
				}
			},
		},
		{
			name: "EventUserInputRequired with prompt_id and choices",
			event: codingagent.StreamEvent{
				Type:     codingagent.EventUserInputRequired,
				Content:  "Which option?",
				PromptID: "prompt-1",
				Choices:  []string{"A", "B"},
			},
			checks: func(t *testing.T, data map[string]any) {
				if data["type"] != "user_input_required" {
					t.Errorf("type = %v, want user_input_required", data["type"])
				}
				if data["content"] != "Which option?" {
					t.Errorf("content = %v", data["content"])
				}
				if data["prompt_id"] != "prompt-1" {
					t.Errorf("prompt_id = %v", data["prompt_id"])
				}
				choices, ok := data["choices"].([]any)
				if !ok || len(choices) != 2 {
					t.Fatalf("choices = %v", data["choices"])
				}
			},
		},
		{
			name: "EventUserInputRequired without choices omits choices field",
			event: codingagent.StreamEvent{
				Type:    codingagent.EventUserInputRequired,
				Content: "Enter value:",
			},
			checks: func(t *testing.T, data map[string]any) {
				if data["type"] != "user_input_required" {
					t.Errorf("type = %v, want user_input_required", data["type"])
				}
				if _, ok := data["choices"]; ok {
					t.Error("choices should be omitted when empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			var data map[string]any
			if err := json.Unmarshal(b, &data); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			tt.checks(t, data)
		})
	}
}

func TestStreamEventJSONUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType codingagent.EventType
	}{
		{
			name:     "text event",
			input:    `{"type":"text","content":"hello"}`,
			wantType: codingagent.EventText,
		},
		{
			name:     "tool_use event",
			input:    `{"type":"tool_use","tool_name":"Read"}`,
			wantType: codingagent.EventToolUse,
		},
		{
			name:     "user_input_required event",
			input:    `{"type":"user_input_required","content":"Pick one","prompt_id":"p1","choices":["yes","no"]}`,
			wantType: codingagent.EventUserInputRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ev codingagent.StreamEvent
			if err := json.Unmarshal([]byte(tt.input), &ev); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if ev.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", ev.Type, tt.wantType)
			}
		})
	}
}

func TestEventTypeConstants(t *testing.T) {
	expected := map[codingagent.EventType]string{
		codingagent.EventText:       "text",
		codingagent.EventToolUse:    "tool_use",
		codingagent.EventToolResult: "tool_result",
		codingagent.EventResult:     "result",
		codingagent.EventError:      "error",
		codingagent.EventSystem:             "system",
		codingagent.EventUserInputRequired:  "user_input_required",
	}

	if len(expected) != 7 {
		t.Fatalf("expected 7 event types, got %d", len(expected))
	}

	for et, want := range expected {
		if string(et) != want {
			t.Errorf("EventType %v = %q, want %q", et, string(et), want)
		}
	}
}
