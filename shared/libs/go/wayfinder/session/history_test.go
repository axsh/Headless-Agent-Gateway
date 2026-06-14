package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendHistory_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	histDir := filepath.Join(dir, "history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	msgs := []Message{
		{Role: "user", Content: "hello", Timestamp: time.Now()},
		{Role: "assistant", Content: "hi there", Timestamp: time.Now()},
	}

	if err := AppendHistory(histDir, msgs, 0); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	// Expect 2 files: 000000001.json and 000000002.json
	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files, got %d", len(entries))
	}
	if entries[0].Name() != "000000001.json" {
		t.Errorf("expected 000000001.json, got %s", entries[0].Name())
	}
	if entries[1].Name() != "000000002.json" {
		t.Errorf("expected 000000002.json, got %s", entries[1].Name())
	}
}

func TestAppendHistory_SequentialNumbering(t *testing.T) {
	dir := t.TempDir()
	histDir := filepath.Join(dir, "history")
	os.MkdirAll(histDir, 0755)

	msgs := []Message{
		{Role: "user", Content: "first"},
	}

	// Start from seq 5 (meaning last written was 5, next is 6)
	if err := AppendHistory(histDir, msgs, 5); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	entries, err := os.ReadDir(histDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if entries[0].Name() != "000000006.json" {
		t.Errorf("expected 000000006.json, got %s", entries[0].Name())
	}
}

func TestAppendHistory_PreservesContent(t *testing.T) {
	dir := t.TempDir()
	histDir := filepath.Join(dir, "history")
	os.MkdirAll(histDir, 0755)

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	msgs := []Message{
		{
			Role:      "assistant",
			Content:   "I'll create a file.",
			Timestamp: now,
			ToolCalls: []ToolCallRecord{
				{ID: "tc-1", Name: "create_file", Input: map[string]any{"path": "test.go"}},
			},
		},
		{
			Role:       "tool",
			Content:    "File created successfully",
			Timestamp:  now.Add(time.Second),
			ToolCallID: "tc-1",
		},
	}

	if err := AppendHistory(histDir, msgs, 0); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	// Read back and verify content
	data, err := os.ReadFile(filepath.Join(histDir, "000000001.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var entry1 HistoryEntry
	if err := json.Unmarshal(data, &entry1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if entry1.Seq != 1 {
		t.Errorf("seq: got %d, want 1", entry1.Seq)
	}
	if entry1.Role != "assistant" {
		t.Errorf("role: got %q, want %q", entry1.Role, "assistant")
	}
	if entry1.Content != "I'll create a file." {
		t.Errorf("content mismatch")
	}
	if len(entry1.ToolCalls) != 1 {
		t.Fatalf("tool_calls: got %d, want 1", len(entry1.ToolCalls))
	}
	if entry1.ToolCalls[0].Name != "create_file" {
		t.Errorf("tool name: got %q, want %q", entry1.ToolCalls[0].Name, "create_file")
	}

	// Verify tool result entry
	data2, err := os.ReadFile(filepath.Join(histDir, "000000002.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var entry2 HistoryEntry
	json.Unmarshal(data2, &entry2)

	if entry2.Role != "tool" {
		t.Errorf("role: got %q, want %q", entry2.Role, "tool")
	}
	if entry2.ToolCallID != "tc-1" {
		t.Errorf("tool_call_id: got %q, want %q", entry2.ToolCallID, "tc-1")
	}
}

func TestLoadHistory_RangeRead(t *testing.T) {
	dir := t.TempDir()
	histDir := filepath.Join(dir, "history")
	os.MkdirAll(histDir, 0755)

	// Write 5 messages
	msgs := []Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
		{Role: "assistant", Content: "msg4"},
		{Role: "user", Content: "msg5"},
	}
	if err := AppendHistory(histDir, msgs, 0); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}

	// Read range [2, 4]
	loaded, err := LoadHistory(histDir, 2, 4)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(loaded))
	}
	if loaded[0].Content != "msg2" {
		t.Errorf("msg[0].Content: got %q, want %q", loaded[0].Content, "msg2")
	}
	if loaded[1].Content != "msg3" {
		t.Errorf("msg[1].Content: got %q, want %q", loaded[1].Content, "msg3")
	}
	if loaded[2].Content != "msg4" {
		t.Errorf("msg[2].Content: got %q, want %q", loaded[2].Content, "msg4")
	}
}

func TestLoadHistory_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	histDir := filepath.Join(dir, "history")
	os.MkdirAll(histDir, 0755)

	// Write only seq 1 and 3 (skip 2)
	msgs1 := []Message{{Role: "user", Content: "msg1"}}
	AppendHistory(histDir, msgs1, 0)

	msgs3 := []Message{{Role: "user", Content: "msg3"}}
	AppendHistory(histDir, msgs3, 2)

	// Read range [1, 3] - seq 2 is missing
	loaded, err := LoadHistory(histDir, 1, 3)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	// Should return 2 messages (1 and 3), skipping missing 2
	if len(loaded) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded))
	}
	if loaded[0].Content != "msg1" {
		t.Errorf("msg[0].Content: got %q, want %q", loaded[0].Content, "msg1")
	}
	if loaded[1].Content != "msg3" {
		t.Errorf("msg[1].Content: got %q, want %q", loaded[1].Content, "msg3")
	}
}

func TestLoadHistory_EmptyRange(t *testing.T) {
	dir := t.TempDir()
	histDir := filepath.Join(dir, "history")
	os.MkdirAll(histDir, 0755)

	// Read from non-existent range
	loaded, err := LoadHistory(histDir, 100, 200)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 messages, got %d", len(loaded))
	}
}
