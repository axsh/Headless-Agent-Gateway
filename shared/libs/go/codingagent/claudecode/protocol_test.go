package claudecode_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
)

func TestParseJSONLinesEvent_System(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNil  bool
		wantType codingagent.EventType
		wantSID  string
	}{
		{
			name:     "system init extracts session_id",
			input:    `{"type":"system","subtype":"init","session_id":"abc-123"}`,
			wantType: codingagent.EventSystem,
			wantSID:  "abc-123",
		},
		{
			name:    "system non-init is ignored",
			input:   `{"type":"system","subtype":"other"}`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := claudecode.ParseJSONLinesEvent(tt.input)
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil, got %+v", ev)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected non-nil event")
			}
			if ev.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", ev.Type, tt.wantType)
			}
			if ev.SessionID != tt.wantSID {
				t.Errorf("SessionID = %v, want %v", ev.SessionID, tt.wantSID)
			}
		})
	}
}

func TestParseJSONLinesEvent_StreamEvent(t *testing.T) {
	input := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventText {
		t.Errorf("Type = %v, want EventText", ev.Type)
	}
	if ev.Content != "hello" {
		t.Errorf("Content = %v, want hello", ev.Content)
	}
}

func TestParseJSONLinesEvent_Assistant(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"path":"main.go"}}]}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolUse {
		t.Errorf("Type = %v, want EventToolUse", ev.Type)
	}
	if ev.ToolName != "Write" {
		t.Errorf("ToolName = %v, want Write", ev.ToolName)
	}
	if ev.ToolInput["path"] != "main.go" {
		t.Errorf("ToolInput[path] = %v, want main.go", ev.ToolInput["path"])
	}
}

func TestParseJSONLinesEvent_ToolResult(t *testing.T) {
	input := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"xxx","content":"ok"}]}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolResult {
		t.Errorf("Type = %v, want EventToolResult", ev.Type)
	}
	if ev.Content != "ok" {
		t.Errorf("Content = %v, want ok", ev.Content)
	}
	if ev.ToolCallID != "xxx" {
		t.Errorf("ToolCallID = %v, want xxx", ev.ToolCallID)
	}
}

func TestParseJSONLinesEvent_BashToolUse_PropagatesID(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"echo hi > a.txt"}}]}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.ToolName != "Bash" {
		t.Errorf("ToolName = %v, want Bash", ev.ToolName)
	}
	if ev.ToolCallID != "tu_1" {
		t.Errorf("ToolCallID = %v, want tu_1", ev.ToolCallID)
	}
}

func TestParseJSONLinesEvent_Result(t *testing.T) {
	input := `{"type":"result","result":"completed"}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventResult {
		t.Errorf("Type = %v, want EventResult", ev.Type)
	}
}

func TestParseJSONLinesEvent_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		wantErr bool
	}{
		{
			name:    "empty line",
			input:   "",
			wantNil: true,
		},
		{
			name:    "invalid JSON",
			input:   "not json",
			wantErr: true,
		},
		{
			name:    "unknown type is ignored",
			input:   `{"type":"unknown_event"}`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := claudecode.ParseJSONLinesEvent(tt.input)
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil, got %+v", ev)
				}
				return
			}
			if tt.wantErr {
				if ev == nil || ev.Type != codingagent.EventError {
					t.Errorf("expected error event, got %+v", ev)
				}
				return
			}
		})
	}
}

// R3: v2.1 text block in assistant message.
func TestParseJSONLinesEvent_V21_TextBlock(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello! I'm here."}]}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventText {
		t.Errorf("Type = %v, want EventText", ev.Type)
	}
	if ev.Content != "Hello! I'm here." {
		t.Errorf("Content = %q, want %q", ev.Content, "Hello! I'm here.")
	}
}

// R2: system/thinking_tokens should be silently ignored.
func TestParseJSONLinesEvent_V21_ThinkingTokens(t *testing.T) {
	input := `{"type":"system","subtype":"thinking_tokens","estimated_tokens":200,"estimated_tokens_delta":24}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev != nil {
		t.Errorf("expected nil for thinking_tokens, got %+v", ev)
	}
}

// R2: assistant/thinking block should not cause errors.
func TestParseJSONLinesEvent_V21_ThinkingBlock(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"reasoning...","signature":"sig123"}]}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	// thinking-only message returns nil (no text or tool_use).
	if ev != nil {
		t.Errorf("expected nil for thinking-only message, got %+v", ev)
	}
}

// R2: v2.1 result with extended fields.
func TestParseJSONLinesEvent_V21_Result(t *testing.T) {
	input := `{"type":"result","subtype":"success","is_error":false,"duration_ms":6354,"num_turns":1,"result":"Hello!","stop_reason":"end_turn","total_cost_usd":0.01,"terminal_reason":"completed","usage":{"input_tokens":12,"output_tokens":8,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventResult {
		t.Errorf("Type = %v, want EventResult", ev.Type)
	}
	if ev.Usage == nil {
		t.Fatal("expected Usage on result")
	}
	if ev.Usage.InputTokens != 12 || ev.Usage.OutputTokens != 8 {
		t.Errorf("usage tokens = %d/%d, want 12/8", ev.Usage.InputTokens, ev.Usage.OutputTokens)
	}
	if ev.Usage.CachedInputTokens != 100 || ev.Usage.CacheCreationInputTokens != 20 {
		t.Errorf("cache = %d/%d", ev.Usage.CachedInputTokens, ev.Usage.CacheCreationInputTokens)
	}
	if ev.Usage.Source != codingagent.UsageSourceClaudeResult {
		t.Errorf("source = %q", ev.Usage.Source)
	}
	if ev.Usage.Confidence != codingagent.UsageConfidenceHigh {
		t.Errorf("confidence = %q", ev.Usage.Confidence)
	}
	if ev.Usage.TotalCostUSD == nil || *ev.Usage.TotalCostUSD != 0.01 {
		t.Errorf("TotalCostUSD = %v", ev.Usage.TotalCostUSD)
	}
}

func TestParseJSONLinesEvent_Result_ModelUsageFallback(t *testing.T) {
	input := `{"type":"result","subtype":"success","usage":{"input_tokens":0,"output_tokens":5},"modelUsage":{"claude-sonnet-4-6":{"inputTokens":0,"outputTokens":10,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0.0001}},"total_cost_usd":0.0001}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil || ev.Usage == nil {
		t.Fatal("expected usage")
	}
	if ev.Usage.OutputTokens != 10 {
		t.Errorf("output = %d, want modelUsage fallback 10", ev.Usage.OutputTokens)
	}
	if ev.Usage.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q", ev.Usage.Model)
	}
}

func TestParseJSONLinesEvent_Assistant_Usage(t *testing.T) {
	input := `{"type":"assistant","message":{"id":"msg_abc","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":100,"output_tokens":5,"cache_read_input_tokens":10,"cache_creation_input_tokens":2}}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Usage == nil {
		t.Fatal("expected Usage on assistant event")
	}
	if ev.Usage.CallID != "msg_abc" {
		t.Errorf("CallID = %q", ev.Usage.CallID)
	}
	if ev.Usage.InputTokens != 100 || ev.Usage.OutputTokens != 5 {
		t.Errorf("tokens = %d/%d", ev.Usage.InputTokens, ev.Usage.OutputTokens)
	}
	if ev.Usage.Source != codingagent.UsageSourceClaudeAssistant {
		t.Errorf("source = %q", ev.Usage.Source)
	}
}


// R3: mixed text and tool_use in assistant message (tool_use takes priority).
func TestParseJSONLinesEvent_V21_TextAndToolUse(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"path":"a.go"}},{"type":"text","text":"ok"}]}}`
	ev := claudecode.ParseJSONLinesEvent(input)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolUse {
		t.Errorf("Type = %v, want EventToolUse", ev.Type)
	}
}

func TestParseJSONLinesEvents_MultipleToolUse(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"path":"a.go"}},{"type":"tool_use","name":"Write","input":{"path":"b.go"}}]}}`
	events := claudecode.ParseJSONLinesEvents(input)
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].ToolInput["path"] != "a.go" {
		t.Errorf("first path = %v", events[0].ToolInput["path"])
	}
	if events[1].ToolInput["path"] != "b.go" {
		t.Errorf("second path = %v", events[1].ToolInput["path"])
	}
}
