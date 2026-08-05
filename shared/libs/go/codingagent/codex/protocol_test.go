package codex_test

import (
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
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
	if ev == nil {
		t.Fatal("expected EventSystem for thread.started")
	}
	if ev.Type != codingagent.EventSystem {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventSystem)
	}
	if ev.SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want abc-123", ev.SessionID)
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

func TestParseExecEvent_FunctionCallOutput(t *testing.T) {
	line := `{"type":"function_call_output","call_id":"fc_123","name":"exec_command","output":"/c/Users/yamya/myprog\n"}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolResult {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
	}
	if ev.Content != "/c/Users/yamya/myprog\n" {
		t.Errorf("content = %q, want pwd output", ev.Content)
	}
}

func TestParseExecEvent_FunctionCallOutput_EmptyOutput(t *testing.T) {
	line := `{"type":"function_call_output"}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolResult {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
	}
	if ev.Content != "" {
		t.Errorf("content = %q, want empty", ev.Content)
	}
}

func TestParseExecEvent_UnknownType(t *testing.T) {
	line := `{"type":"some.future.event","data":"hello"}`
	ev := codex.ParseExecEvent(line)
	if ev != nil {
		t.Errorf("expected nil for unknown event type, got %+v", ev)
	}
}

// --- Nested JSONL format tests (Codex CLI 0.139.0+) ---

func TestParseExecEvent_ResponseItem_FunctionCall(t *testing.T) {
	line := `{"timestamp":"2026-06-12T07:21:11.017Z","type":"response_item","payload":{"type":"function_call","name":"shell_command","arguments":"{\"command\": \"pwd\"}","call_id":"fc_123"}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolUse {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolUse)
	}
	if ev.ToolName != "shell_command" {
		t.Errorf("tool_name = %q, want %q", ev.ToolName, "shell_command")
	}
}

func TestParseExecEvent_ResponseItem_FunctionCallOutput(t *testing.T) {
	line := `{"timestamp":"2026-06-12T07:21:12.216Z","type":"response_item","payload":{"type":"function_call_output","call_id":"fc_123","output":"Exit code: 0\nOutput:\n/home/user\n"}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolResult {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
	}
	if !strings.Contains(ev.Content, "/home/user") {
		t.Errorf("content = %q, want to contain /home/user", ev.Content)
	}
}

func TestParseExecEvent_ResponseItem_AssistantMessage(t *testing.T) {
	line := `{"timestamp":"2026-06-12T07:21:13.903Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The current directory is /home/user."}]}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventText {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventText)
	}
	if !strings.Contains(ev.Content, "The current directory") {
		t.Errorf("content = %q, want to contain 'The current directory'", ev.Content)
	}
}

func TestParseExecEvent_EventMsg_AgentMessage(t *testing.T) {
	line := `{"timestamp":"2026-06-12T07:21:13.903Z","type":"event_msg","payload":{"type":"agent_message","message":"The current directory is /home/user."}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventText {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventText)
	}
	if ev.Content != "The current directory is /home/user." {
		t.Errorf("content = %q, want exact message", ev.Content)
	}
}

func TestParseExecEvent_EventMsg_TaskComplete(t *testing.T) {
	line := `{"timestamp":"2026-06-12T07:21:13.907Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventResult {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventResult)
	}
}

func TestParseExecEvent_EventMsg_Ignored(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"token_count"}}`,
		`{"type":"event_msg","payload":{"type":"user_message"}}`,
		`{"type":"event_msg","payload":{"type":"task_started"}}`,
	}
	for _, line := range lines {
		ev := codex.ParseExecEvent(line)
		if ev != nil {
			t.Errorf("expected nil for %s, got %+v", line, ev)
		}
	}
}

// --- item.started / item.completed format tests (Codex CLI 0.139.0 actual stdout) ---
// These are the events that `codex exec --json` actually outputs to stdout.
// The response_item/event_msg format is used in session rollout logs but NOT in stdout.

func TestParseExecEvent_ItemStarted_CommandExecution(t *testing.T) {
	line := `{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"echo hello","aggregated_output":"","exit_code":null,"status":"in_progress"}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolUse {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolUse)
	}
	if ev.ToolName != "command_execution" {
		t.Errorf("tool_name = %q, want %q", ev.ToolName, "command_execution")
	}
}

func TestParseExecEvent_ItemCompleted_CommandExecution(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"echo hello","aggregated_output":"hello\r\n","exit_code":0,"status":"completed"}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventToolResult {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
	}
	if !strings.Contains(ev.Content, "hello") {
		t.Errorf("content = %q, want to contain 'hello'", ev.Content)
	}
}

func TestParseExecEvent_ItemCompleted_AgentMessage(t *testing.T) {
	line := `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"The command was executed successfully."}}`
	ev := codex.ParseExecEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Type != codingagent.EventText {
		t.Errorf("type = %q, want %q", ev.Type, codingagent.EventText)
	}
	if !strings.Contains(ev.Content, "The command was executed successfully.") {
		t.Errorf("content = %q, want to contain message text", ev.Content)
	}
}
