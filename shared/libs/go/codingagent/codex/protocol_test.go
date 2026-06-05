package codex_test

import (
	"encoding/json"
	"testing"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/codex"
)

func TestBuildInitializeRequest(t *testing.T) {
	data, err := codex.BuildInitializeRequest()
	if err != nil {
		t.Fatalf("BuildInitializeRequest error: %v", err)
	}

	var msg codex.JSONRPCMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if msg.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", msg.JSONRPC)
	}
	if msg.Method != "initialize" {
		t.Errorf("method = %v, want initialize", msg.Method)
	}
	if msg.ID == nil || *msg.ID != 1 {
		t.Errorf("id = %v, want 1", msg.ID)
	}
}

func TestBuildStartThreadRequest(t *testing.T) {
	data, err := codex.BuildStartThreadRequest("hello world")
	if err != nil {
		t.Fatalf("BuildStartThreadRequest error: %v", err)
	}

	var msg codex.JSONRPCMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if msg.Method != "startThread" {
		t.Errorf("method = %v, want startThread", msg.Method)
	}

	var params map[string]string
	json.Unmarshal(msg.Params, &params)
	if params["prompt"] != "hello world" {
		t.Errorf("params.prompt = %v, want hello world", params["prompt"])
	}
}

func TestParseNotification(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType codingagent.EventType
		wantText string
		wantTool string
	}{
		{
			name:     "text notification",
			input:    `{"jsonrpc":"2.0","method":"text","params":{"content":"hello"}}`,
			wantType: codingagent.EventText,
			wantText: "hello",
		},
		{
			name:     "tool_use notification",
			input:    `{"jsonrpc":"2.0","method":"tool_use","params":{"name":"Write","input":{"path":"a.go"}}}`,
			wantType: codingagent.EventToolUse,
			wantTool: "Write",
		},
		{
			name:     "result notification",
			input:    `{"jsonrpc":"2.0","method":"result"}`,
			wantType: codingagent.EventResult,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := codex.ParseNotification(tt.input)
			if ev == nil {
				t.Fatal("expected non-nil event")
			}
			if ev.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", ev.Type, tt.wantType)
			}
			if tt.wantText != "" && ev.Content != tt.wantText {
				t.Errorf("Content = %v, want %v", ev.Content, tt.wantText)
			}
			if tt.wantTool != "" && ev.ToolName != tt.wantTool {
				t.Errorf("ToolName = %v, want %v", ev.ToolName, tt.wantTool)
			}
		})
	}
}

func TestParseApprovalRequest(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"approval_request","id":5,"params":{"tool":"Write"}}`
	var msg codex.JSONRPCMessage
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !codex.IsApprovalRequest(&msg) {
		t.Error("should be recognized as approval request")
	}

	resp, err := codex.BuildApprovalResponse(*msg.ID)
	if err != nil {
		t.Fatalf("BuildApprovalResponse error: %v", err)
	}

	var respMsg codex.JSONRPCMessage
	json.Unmarshal(resp, &respMsg)
	if respMsg.ID == nil || *respMsg.ID != 5 {
		t.Errorf("response id = %v, want 5", respMsg.ID)
	}

	var result map[string]bool
	json.Unmarshal(respMsg.Result, &result)
	if !result["approved"] {
		t.Error("response should have approved=true")
	}
}

func TestParseNotification_Unknown(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"unknown_method"}`
	ev := codex.ParseNotification(input)
	if ev != nil {
		t.Errorf("expected nil for unknown method, got %+v", ev)
	}
}
