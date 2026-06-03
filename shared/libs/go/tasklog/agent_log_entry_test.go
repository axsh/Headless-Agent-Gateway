package tasklog

import (
	"encoding/json"
	"testing"
)

func TestNewAgentLogEntry(t *testing.T) {
	agentID := "agent-123"
	entry := NewAgentLogEntry(agentID, WithKind("thinking"), WithLocation("test.go:10"))

	if entry.AgentID != agentID {
		t.Errorf("expected AgentID %q, got %q", agentID, entry.AgentID)
	}
	if entry.Kind != "thinking" {
		t.Errorf("expected Kind %q, got %q", "thinking", entry.Kind)
	}
	if entry.Location != "test.go:10" {
		t.Errorf("expected Location %q, got %q", "test.go:10", entry.Location)
	}
	if entry.ID == "" {
		t.Errorf("expected generated UUID, got empty")
	}
	if entry.Phase != "begin" {
		t.Errorf("expected Phase 'begin', got %q", entry.Phase)
	}
}

func TestNewAgentLogSendEntry(t *testing.T) {
	logID := "log-456"
	agentID := "agent-123"
	body := "hello chunk"
	entry := NewAgentLogSendEntry(logID, agentID, body)

	if entry.ID != logID {
		t.Errorf("expected ID %q, got %q", logID, entry.ID)
	}
	if entry.AgentID != agentID {
		t.Errorf("expected AgentID %q, got %q", agentID, entry.AgentID)
	}
	if entry.Body != body {
		t.Errorf("expected Body %q, got %q", body, entry.Body)
	}
	if entry.Phase != "send" {
		t.Errorf("expected Phase 'send', got %q", entry.Phase)
	}
}

func TestNewAgentLogEndEntry(t *testing.T) {
	logID := "log-456"
	agentID := "agent-123"
	entry := NewAgentLogEndEntry(logID, agentID)

	if entry.ID != logID {
		t.Errorf("expected ID %q, got %q", logID, entry.ID)
	}
	if entry.AgentID != agentID {
		t.Errorf("expected AgentID %q, got %q", agentID, entry.AgentID)
	}
	if !entry.IsComplete {
		t.Errorf("expected IsComplete to be true")
	}
	if entry.Phase != "end" {
		t.Errorf("expected Phase 'end', got %q", entry.Phase)
	}
}

func TestAgentLogEntry_JSONSerialization(t *testing.T) {
	entry := &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        "log-id",
			EntryType: AgentLogEntryType,
		},
		AgentID:     "agent-id",
		ParentLogID: "parent-id",
		Phase:       "begin",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("JSON marshaling failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON unmarshaling failed: %v", err)
	}

	if parsed["parentLogId"] != "parent-id" {
		t.Errorf("expected parentLogId JSON field to be 'parent-id', got %v", parsed["parentLogId"])
	}
	if parsed["agentId"] != "agent-id" {
		t.Errorf("expected agentId JSON field to be 'agent-id', got %v", parsed["agentId"])
	}
}
