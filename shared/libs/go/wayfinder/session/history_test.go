package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestAppendHistory_HexFilenames(t *testing.T) {
	histDir := t.TempDir()
	msgs := []Message{
		{Role: "user", Content: "hello", Seq: 1, Timestamp: time.Now()},
		{Role: "assistant", Content: "hi", Seq: 2, Timestamp: time.Now()},
		{Role: "user", Content: "bye", Seq: 3, Timestamp: time.Now()},
	}

	err := AppendHistory(histDir, msgs)
	if err != nil {
		t.Fatalf("AppendHistory failed: %v", err)
	}

	// Verify files exist with 7-digit hex names.
	expectedFiles := []string{"0000001.json", "0000002.json", "0000003.json"}
	for _, f := range expectedFiles {
		path := filepath.Join(histDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}

	// Verify file content.
	data, err := os.ReadFile(filepath.Join(histDir, "0000001.json"))
	if err != nil {
		t.Fatalf("failed to read history file: %v", err)
	}
	var entry HistoryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse history entry: %v", err)
	}
	if entry.Seq != 1 {
		t.Errorf("expected seq=1, got %d", entry.Seq)
	}
	if entry.Role != "user" {
		t.Errorf("expected role=user, got %s", entry.Role)
	}
}

func TestAppendHistory_SubDir(t *testing.T) {
	histDir := t.TempDir()
	subDir := filepath.Join(histDir, "000000a")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	msgs := []Message{
		{Role: "user", Content: "sub-hello", Seq: 1, Timestamp: time.Now()},
		{Role: "assistant", Content: "sub-hi", Seq: 2, Timestamp: time.Now()},
	}

	err := AppendHistory(subDir, msgs)
	if err != nil {
		t.Fatalf("AppendHistory in subdir failed: %v", err)
	}

	// Verify files exist inside subdirectory (not with prefix).
	expectedFiles := []string{"0000001.json", "0000002.json"}
	for _, f := range expectedFiles {
		path := filepath.Join(subDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s in subdir", f)
		}
	}

	// Verify no files leaked to parent directory.
	entries, _ := os.ReadDir(histDir)
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("unexpected file in parent dir: %s", e.Name())
		}
	}
}

func TestAppendHistory_SkipExisting(t *testing.T) {
	histDir := t.TempDir()

	// Pre-create file for seq=1.
	existingPath := filepath.Join(histDir, "0000001.json")
	if err := os.WriteFile(existingPath, []byte(`{"seq":1,"role":"user","content":"original"}`), 0644); err != nil {
		t.Fatalf("failed to create pre-existing file: %v", err)
	}

	msgs := []Message{
		{Role: "user", Content: "replacement", Seq: 1, Timestamp: time.Now()},
		{Role: "assistant", Content: "new", Seq: 2, Timestamp: time.Now()},
	}

	err := AppendHistory(histDir, msgs)
	if err != nil {
		t.Fatalf("AppendHistory failed: %v", err)
	}

	// Seq=1 should not be overwritten.
	data, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("failed to read existing file: %v", err)
	}
	var entry HistoryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse history entry: %v", err)
	}
	if entry.Content != "original" {
		t.Errorf("existing file was overwritten: expected 'original', got '%s'", entry.Content)
	}

	// Seq=2 should be created.
	newPath := filepath.Join(histDir, "0000002.json")
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("expected new file 0000002.json to exist")
	}
}

func TestAppendHistory_HexFilenamePattern(t *testing.T) {
	histDir := t.TempDir()
	msgs := []Message{
		{Role: "user", Content: "test", Seq: 255, Timestamp: time.Now()},
	}

	err := AppendHistory(histDir, msgs)
	if err != nil {
		t.Fatalf("AppendHistory failed: %v", err)
	}

	// 255 = 0xff -> 00000ff.json
	expectedFile := "00000ff.json"
	path := filepath.Join(histDir, expectedFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist for seq=255 (0xff)", expectedFile)
	}

	// Verify pattern matches 7-digit hex.
	hexPattern := regexp.MustCompile(`^[0-9a-f]{7}\.json$`)
	entries, _ := os.ReadDir(histDir)
	for _, e := range entries {
		if !hexPattern.MatchString(e.Name()) {
			t.Errorf("filename %s does not match 7-digit hex pattern", e.Name())
		}
	}
}

func TestStore_SaveWithSeqBasedHistory(t *testing.T) {
	rootDir := t.TempDir()
	store := NewStore(rootDir)

	state := &SessionState{
		SessionID: "test-session",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "first", Seq: 1, Timestamp: time.Now()},
			{Role: "assistant", Content: "reply", Seq: 2, Timestamp: time.Now()},
		},
		CreatedAt: time.Now(),
	}

	// First save: both messages are new.
	if err := store.Save(state); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	histDir := filepath.Join(rootDir, "test-session", "history")
	if _, err := os.Stat(filepath.Join(histDir, "0000001.json")); os.IsNotExist(err) {
		t.Error("expected 0000001.json after first save")
	}
	if _, err := os.Stat(filepath.Join(histDir, "0000002.json")); os.IsNotExist(err) {
		t.Error("expected 0000002.json after first save")
	}

	// Second save with one new message.
	state.Messages = append(state.Messages, Message{
		Role: "user", Content: "second", Seq: 3, Timestamp: time.Now(),
	})
	if err := store.Save(state); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(histDir, "0000003.json")); os.IsNotExist(err) {
		t.Error("expected 0000003.json after second save")
	}

	// Verify metadata TotalSeq.
	metaData, err := os.ReadFile(filepath.Join(rootDir, "test-session", "metadata.json"))
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}
	var meta SessionMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if meta.TotalSeq != 3 {
		t.Errorf("expected TotalSeq=3, got %d", meta.TotalSeq)
	}
}

func TestStore_SaveAfterCompaction_NoOverwrite(t *testing.T) {
	rootDir := t.TempDir()
	store := NewStore(rootDir)

	// Save initial state with 4 messages.
	state := &SessionState{
		SessionID: "compaction-test",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "msg1", Seq: 1, Timestamp: time.Now()},
			{Role: "assistant", Content: "msg2", Seq: 2, Timestamp: time.Now()},
			{Role: "user", Content: "msg3", Seq: 3, Timestamp: time.Now()},
			{Role: "assistant", Content: "msg4", Seq: 4, Timestamp: time.Now()},
		},
		CreatedAt: time.Now(),
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("initial save failed: %v", err)
	}

	// Simulate compaction: reduce messages to 2 (summary + recent).
	state.Messages = []Message{
		{Role: "system", Content: "summary", Seq: 0, Pinned: true, Timestamp: time.Now()},
		{Role: "user", Content: "msg3", Seq: 3, Timestamp: time.Now()},
		{Role: "assistant", Content: "msg4", Seq: 4, Timestamp: time.Now()},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("save after compaction failed: %v", err)
	}

	// History files should still exist for seq 1-4.
	histDir := filepath.Join(rootDir, "compaction-test", "history")
	for seq := 1; seq <= 4; seq++ {
		filename := filepath.Join(histDir, fmt.Sprintf("%07x.json", seq))
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			t.Errorf("expected history file for seq=%d to still exist after compaction", seq)
		}
	}

	// Verify TotalSeq is still 4 (not decreased).
	metaData, err := os.ReadFile(filepath.Join(rootDir, "compaction-test", "metadata.json"))
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}
	var meta SessionMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if meta.TotalSeq != 4 {
		t.Errorf("expected TotalSeq=4 after compaction, got %d", meta.TotalSeq)
	}
}

func TestAppendHistory_WritesOrigin(t *testing.T) {
	histDir := t.TempDir()
	err := AppendHistory(histDir, []Message{
		{Role: "user", Content: "hi", Seq: 1, Origin: OriginClaudeCode, Timestamp: time.Now()},
	})
	if err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(histDir, "0000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entry HistoryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Origin != OriginClaudeCode {
		t.Errorf("origin = %q, want %q", entry.Origin, OriginClaudeCode)
	}
}

func TestLoadHistory_MissingOriginDefaultsWayfinder(t *testing.T) {
	histDir := t.TempDir()
	path := filepath.Join(histDir, "0000001.json")
	if err := os.WriteFile(path, []byte(`{"seq":1,"role":"user","content":"old"}`), 0644); err != nil {
		t.Fatal(err)
	}
	msgs, err := LoadHistory(histDir, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Origin != OriginWayfinder {
		t.Errorf("Origin = %q, want %q", msgs[0].Origin, OriginWayfinder)
	}
}

func TestAppendHistory_DoesNotOverwriteExisting(t *testing.T) {
	histDir := t.TempDir()
	existing := `{"seq":1,"role":"user","content":"keep","origin":"claudecode"}`
	if err := os.WriteFile(filepath.Join(histDir, "0000001.json"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	err := AppendHistory(histDir, []Message{
		{Role: "user", Content: "changed", Seq: 1, Origin: OriginCodex, Timestamp: time.Now()},
	})
	if err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(histDir, "0000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entry HistoryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Origin != OriginClaudeCode {
		t.Errorf("origin changed to %q", entry.Origin)
	}
	if entry.Content != "keep" {
		t.Errorf("content = %q, want keep", entry.Content)
	}
}

func TestStore_WithSubDir(t *testing.T) {
	rootDir := t.TempDir()
	store := NewStore(rootDir)
	childStore := store.WithSubDir("000000a")

	if childStore.subDir != "000000a" {
		t.Errorf("expected subDir=000000a, got %s", childStore.subDir)
	}
	if childStore.rootDir != rootDir {
		t.Errorf("expected rootDir=%s, got %s", rootDir, childStore.rootDir)
	}
}

func TestStore_SaveWithSubDir(t *testing.T) {
	rootDir := t.TempDir()
	store := NewStore(rootDir).WithSubDir("000000a")

	state := &SessionState{
		SessionID: "test-session",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "child msg", Seq: 1, Timestamp: time.Now()},
			{Role: "assistant", Content: "child reply", Seq: 2, Timestamp: time.Now()},
		},
		CreatedAt: time.Now(),
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("save with subDir failed: %v", err)
	}

	// Verify history files are in subdirectory.
	for _, seq := range []int{1, 2} {
		histPath := filepath.Join(rootDir, "test-session", "history", "000000a", fmt.Sprintf("%07x.json", seq))
		if _, err := os.Stat(histPath); os.IsNotExist(err) {
			t.Errorf("expected history file at %s", histPath)
		}
	}

	// Verify no files at root history level.
	rootHistDir := filepath.Join(rootDir, "test-session", "history")
	entries, _ := os.ReadDir(rootHistDir)
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("unexpected file at root history level: %s", e.Name())
		}
	}
}
