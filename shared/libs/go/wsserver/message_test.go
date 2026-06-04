package wsserver

import (
	"encoding/json"
	"testing"

	"github.com/axsh/hag/tasklog"
)

func TestNewLogMessage(t *testing.T) {
	entry := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("thinking"),
		tasklog.WithParentLogID("root-uuid"),
	)
	data, err := NewLogMessage(entry)
	if err != nil {
		t.Fatalf("NewLogMessage error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if msg.Type != "log" {
		t.Errorf("Type = %q, want %q", msg.Type, "log")
	}

	var payload LogPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload.Entry.Kind != "thinking" {
		t.Errorf("Kind = %q, want %q", payload.Entry.Kind, "thinking")
	}
	if payload.Entry.ParentLogID != "root-uuid" {
		t.Errorf("ParentLogID = %q, want %q", payload.Entry.ParentLogID, "root-uuid")
	}
	if payload.Entry.Phase != "begin" {
		t.Errorf("Phase = %q, want %q", payload.Entry.Phase, "begin")
	}
	if payload.Entry.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", payload.Entry.AgentID, "agent-1")
	}
}

func TestNewLogMessage_SendPhase(t *testing.T) {
	entry := tasklog.NewAgentLogSendEntry("log-1", "agent-1", "hello world")
	data, err := NewLogMessage(entry)
	if err != nil {
		t.Fatalf("NewLogMessage error: %v", err)
	}

	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Type != "log" {
		t.Errorf("Type = %q, want %q", msg.Type, "log")
	}

	var payload LogPayload
	json.Unmarshal(msg.Payload, &payload)
	if payload.Entry.Phase != "send" {
		t.Errorf("Phase = %q, want %q", payload.Entry.Phase, "send")
	}
	if payload.Entry.Body != "hello world" {
		t.Errorf("Body = %q, want %q", payload.Entry.Body, "hello world")
	}
}

func TestNewLogMessage_EndPhase(t *testing.T) {
	entry := tasklog.NewAgentLogEndEntry("log-1", "agent-1")
	data, err := NewLogMessage(entry)
	if err != nil {
		t.Fatalf("NewLogMessage error: %v", err)
	}

	var msg Message
	json.Unmarshal(data, &msg)

	var payload LogPayload
	json.Unmarshal(msg.Payload, &payload)
	if payload.Entry.Phase != "end" {
		t.Errorf("Phase = %q, want %q", payload.Entry.Phase, "end")
	}
	if !payload.Entry.IsComplete {
		t.Error("IsComplete should be true for end phase")
	}
}

func TestNewSnapshotMessage(t *testing.T) {
	entries := []tasklog.Entry{
		tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text")),
		tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("thinking")),
	}
	data, err := NewSnapshotMessage(entries)
	if err != nil {
		t.Fatalf("NewSnapshotMessage error: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if msg.Type != "snapshot" {
		t.Errorf("Type = %q, want %q", msg.Type, "snapshot")
	}
}

func TestNewSnapshotMessage_Empty(t *testing.T) {
	data, err := NewSnapshotMessage(nil)
	if err != nil {
		t.Fatalf("NewSnapshotMessage error: %v", err)
	}

	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Type != "snapshot" {
		t.Errorf("Type = %q, want %q", msg.Type, "snapshot")
	}
}
