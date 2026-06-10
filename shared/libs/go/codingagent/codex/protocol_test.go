package codex_test

import (
	"testing"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/codex"
)

func TestParseExecEvent_Error(t *testing.T) {
	line := `{"type":"error","message":"connection failed"}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventError {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventError)
	}
	if ev.Content != "connection failed" {
		t.Errorf("content = %q, want %q", ev.Content, "connection failed")
	}
}

func TestParseExecEvent_TurnFailed(t *testing.T) {
	line := `{"type":"turn.failed","error":{"message":"401 Unauthorized"}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventError {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventError)
	}
	if ev.Content != "401 Unauthorized" {
		t.Errorf("content = %q, want %q", ev.Content, "401 Unauthorized")
	}
}

func TestParseExecEvent_TurnCompleted(t *testing.T) {
	line := `{"type":"turn.completed"}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventResult {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventResult)
	}
}

func TestParseExecEvent_ThreadStarted(t *testing.T) {
	line := `{"type":"thread.started","thread_id":"abc-123"}`
	ev := codex.ParseExecEvent(line)
	if ev != nil {
		t.Errorf("expected nil for thread.started, got %+v", ev)
	}
}

func TestParseExecEvent_TurnStarted(t *testing.T) {
	line := `{"type":"turn.started"}`
	ev := codex.ParseExecEvent(line)
	if ev != nil {
		t.Errorf("expected nil for turn.started, got %+v", ev)
	}
}

func TestParseExecEvent_TextDelta(t *testing.T) {
	line := `{"type":"response.output_text.delta","delta":"hello "}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventText {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventText)
	}
	if ev.Content != "hello " {
		t.Errorf("content = %q, want %q", ev.Content, "hello ")
	}
}

func TestParseExecEvent_FunctionCall(t *testing.T) {
	line := `{"type":"function_call","name":"shell","arguments":"{\"command\":\"ls\"}"}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolUse {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolUse)
	}
	if ev.ToolName != "shell" {
		t.Errorf("tool_name = %q, want %q", ev.ToolName, "shell")
	}
}

func TestParseExecEvent_InvalidJSON(t *testing.T) {
	ev := codex.ParseExecEvent("not json")
	if ev != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", ev)
	}
}
