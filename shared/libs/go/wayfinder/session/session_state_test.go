package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionState_Serialization(t *testing.T) {
	parentID := "parent-123"
	now := time.Now().Truncate(time.Millisecond)

	original := &SessionState{
		SessionID: "session-001",
		ParentID:  &parentID,
		Status:    StatusActive,
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant.", Timestamp: now, Pinned: true},
			{Role: "user", Content: "Hello", Timestamp: now},
			{Role: "assistant", Content: "Hi there!", Timestamp: now, ToolCalls: []ToolCallRecord{
				{ID: "tc-1", Name: "read_file", Input: map[string]any{"path": "test.txt"}},
			}},
			{Role: "tool", Content: "file content", Timestamp: now, ToolCallID: "tc-1"},
		},
		CreatedFiles: []TrackedFile{
			{Path: "/tmp/test/file.go", CreatedAt: now, IsDir: false},
			{Path: "/tmp/test/dir", CreatedAt: now, IsDir: true},
		},
		RunningProcesses: []TrackedProcess{
			{PID: 12345, Command: "sleep", Args: []string{"100"}, StartedAt: now},
		},
		CreatedAt:      now,
		LastActivityAt: now,
	}

	// Serialize.
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Deserialize.
	var restored SessionState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify all fields.
	if restored.SessionID != original.SessionID {
		t.Errorf("SessionID = %q, want %q", restored.SessionID, original.SessionID)
	}
	if restored.ParentID == nil || *restored.ParentID != parentID {
		t.Errorf("ParentID = %v, want %q", restored.ParentID, parentID)
	}
	if restored.Status != original.Status {
		t.Errorf("Status = %q, want %q", restored.Status, original.Status)
	}
	if len(restored.Messages) != 4 {
		t.Fatalf("len(Messages) = %d, want 4", len(restored.Messages))
	}
	if restored.Messages[0].Pinned != true {
		t.Error("Messages[0].Pinned should be true")
	}
	if len(restored.Messages[2].ToolCalls) != 1 {
		t.Fatalf("len(Messages[2].ToolCalls) = %d, want 1", len(restored.Messages[2].ToolCalls))
	}
	if restored.Messages[2].ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", restored.Messages[2].ToolCalls[0].Name, "read_file")
	}
	if restored.Messages[3].ToolCallID != "tc-1" {
		t.Errorf("Messages[3].ToolCallID = %q, want %q", restored.Messages[3].ToolCallID, "tc-1")
	}
	if len(restored.CreatedFiles) != 2 {
		t.Fatalf("len(CreatedFiles) = %d, want 2", len(restored.CreatedFiles))
	}
	if restored.CreatedFiles[1].IsDir != true {
		t.Error("CreatedFiles[1].IsDir should be true")
	}
	if len(restored.RunningProcesses) != 1 {
		t.Fatalf("len(RunningProcesses) = %d, want 1", len(restored.RunningProcesses))
	}
	if restored.RunningProcesses[0].PID != 12345 {
		t.Errorf("RunningProcesses[0].PID = %d, want 12345", restored.RunningProcesses[0].PID)
	}
}

func TestSessionState_ParentID_Nil(t *testing.T) {
	state := &SessionState{SessionID: "s-1", Status: StatusCompleted}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	// parent_id should be omitted when nil.
	var m map[string]any
	json.Unmarshal(data, &m)
	if _, exists := m["parent_id"]; exists {
		t.Error("parent_id should be omitted when nil")
	}
}

func TestSessionState_ParentID_Set(t *testing.T) {
	parentID := "p-abc"
	state := &SessionState{SessionID: "s-2", ParentID: &parentID, Status: StatusActive}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var restored SessionState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if restored.ParentID == nil || *restored.ParentID != parentID {
		t.Errorf("ParentID = %v, want %q", restored.ParentID, parentID)
	}
}
func TestSessionState_MultimodalSerialization(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	state := &SessionState{
		SessionID: "s-multi",
		Status:    StatusActive,
		Messages: []Message{
			{
				Role: "user",
				ContentParts: []ContentPart{
					{Type: "text", Text: "Look at this image:"},
					{Type: "image", Image: &ImageMetadata{Path: "multimodal/abc.png", MediaType: "image/png"}},
				},
				Timestamp: now,
			},
		},
		CreatedAt: now,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored SessionState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(restored.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(restored.Messages))
	}
	parts := restored.Messages[0].ContentParts
	if len(parts) != 2 {
		t.Fatalf("len(ContentParts) = %d, want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "Look at this image:" {
		t.Errorf("parts[0] = %+v", parts[0])
	}
	if parts[1].Type != "image" || parts[1].Image == nil || parts[1].Image.Path != "multimodal/abc.png" {
		t.Errorf("parts[1] = %+v", parts[1])
	}
}
