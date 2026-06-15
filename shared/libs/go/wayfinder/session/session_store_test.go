package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	now := time.Now().Truncate(time.Millisecond)
	original := &SessionState{
		SessionID: "test-save-load",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "hello", Timestamp: now},
			{Role: "assistant", Content: "world", Timestamp: now},
		},
		CreatedFiles: []TrackedFile{
			{Path: "/tmp/file.txt", CreatedAt: now},
		},
		RunningProcesses: []TrackedProcess{
			{PID: 999, Command: "sleep", StartedAt: now},
		},
		CreatedAt: now,
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load("test-save-load")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.SessionID != original.SessionID {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, original.SessionID)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("len(Messages) = %d, want 2", len(loaded.Messages))
	}
	if len(loaded.CreatedFiles) != 1 {
		t.Errorf("len(CreatedFiles) = %d, want 1", len(loaded.CreatedFiles))
	}
	if len(loaded.RunningProcesses) != 1 {
		t.Errorf("len(RunningProcesses) = %d, want 1", len(loaded.RunningProcesses))
	}
	// LastActivityAt should be updated by Save.
	if loaded.LastActivityAt.IsZero() {
		t.Error("LastActivityAt should be set by Save")
	}
}

func TestStore_Load_NotExists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	state, err := store.Load("nonexistent-session")
	if err != nil {
		t.Fatalf("Load should not error for non-existent: %v", err)
	}
	if state != nil {
		t.Error("Load should return nil for non-existent session")
	}
}

func TestStore_AtomicWrite_NoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	state := &SessionState{SessionID: "atomic-test", Status: StatusActive}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Check that no .tmp file remains in session folder.
	sessionDir := filepath.Join(dir, "atomic-test")
	entries, _ := os.ReadDir(sessionDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file found: %s", e.Name())
		}
	}

	// Check that metadata.json exists in session folder.
	if _, err := os.Stat(filepath.Join(sessionDir, "metadata.json")); err != nil {
		t.Errorf("metadata.json should exist: %v", err)
	}
}

func TestStore_Cleanup(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create an old legacy file with modified time in the past.
	oldFile := filepath.Join(dir, "old-session.json")
	os.WriteFile(oldFile, []byte(`{}`), 0644)
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldFile, oldTime, oldTime)

	// Create a new session (folder format).
	newState := &SessionState{SessionID: "new-session", Status: StatusActive}
	store.Save(newState)

	removed, err := store.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	// Old file should be gone.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Error("old file should have been removed")
	}
	// New session folder should still exist.
	if _, err := os.Stat(filepath.Join(dir, "new-session", "metadata.json")); err != nil {
		t.Error("new session folder should still exist")
	}
}

func TestStore_SaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "session", "dir")
	store := NewStore(dir)

	state := &SessionState{SessionID: "auto-dir", Status: StatusActive}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save should create dir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "auto-dir", "metadata.json")); err != nil {
		t.Errorf("metadata.json should exist after auto-dir creation: %v", err)
	}
}

// --- New folder-based tests ---

func TestStore_SaveAndLoad_FolderStructure(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	state := &SessionState{
		SessionID: "folder-test",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sessionDir := filepath.Join(dir, "folder-test")

	// Verify folder structure exists.
	if _, err := os.Stat(filepath.Join(sessionDir, "metadata.json")); err != nil {
		t.Error("metadata.json should exist")
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "context.json")); err != nil {
		t.Error("context.json should exist")
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "history")); err != nil {
		t.Error("history/ should exist")
	}

	// Verify history files.
	histEntries, _ := os.ReadDir(filepath.Join(sessionDir, "history"))
	if len(histEntries) != 2 {
		t.Errorf("expected 2 history files, got %d", len(histEntries))
	}
}

func TestStore_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	now := time.Now().Truncate(time.Millisecond)
	original := &SessionState{
		SessionID: "roundtrip-test",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "hello", Timestamp: now},
			{Role: "assistant", Content: "world", Timestamp: now},
		},
		CreatedAt: now,
	}
	store.Save(original)

	loaded, err := store.Load("roundtrip-test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SessionID != original.SessionID {
		t.Errorf("SessionID mismatch")
	}
	if loaded.Status != original.Status {
		t.Errorf("Status mismatch")
	}
	if len(loaded.Messages) != len(original.Messages) {
		t.Errorf("Messages count: got %d, want %d", len(loaded.Messages), len(original.Messages))
	}
	for i, msg := range loaded.Messages {
		if msg.Content != original.Messages[i].Content {
			t.Errorf("Message[%d].Content: got %q, want %q", i, msg.Content, original.Messages[i].Content)
		}
	}
}

func TestStore_MigrateLegacy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create a legacy single-file session.
	legacyState := &SessionState{
		SessionID: "legacy-session",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "old msg"},
		},
	}
	legacyData, _ := json.MarshalIndent(legacyState, "", "  ")
	os.WriteFile(filepath.Join(dir, "legacy-session.json"), legacyData, 0644)

	// Load should trigger auto-migration.
	loaded, err := store.Load("legacy-session")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.SessionID != "legacy-session" {
		t.Errorf("SessionID: got %q", loaded.SessionID)
	}
	if len(loaded.Messages) != 1 {
		t.Errorf("Messages: got %d, want 1", len(loaded.Messages))
	}

	// Verify migration created folder structure.
	if _, err := os.Stat(filepath.Join(dir, "legacy-session", "metadata.json")); err != nil {
		t.Error("migration should create metadata.json")
	}

	// Verify legacy file was renamed to .bak.
	if _, err := os.Stat(filepath.Join(dir, "legacy-session.json.bak")); err != nil {
		t.Error("legacy file should be renamed to .bak")
	}
}

func TestStore_MigrateLegacy_DataIntegrity(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	now := time.Now().Truncate(time.Millisecond)
	legacyState := &SessionState{
		SessionID: "integrity-test",
		Status:    StatusCompleted,
		Messages: []Message{
			{Role: "user", Content: "first", Timestamp: now},
			{Role: "assistant", Content: "second", Timestamp: now},
			{Role: "user", Content: "third", Timestamp: now},
		},
		CreatedAt: now,
	}
	legacyData, _ := json.MarshalIndent(legacyState, "", "  ")
	os.WriteFile(filepath.Join(dir, "integrity-test.json"), legacyData, 0644)

	loaded, _ := store.Load("integrity-test")

	if loaded.Status != StatusCompleted {
		t.Errorf("Status: got %q, want %q", loaded.Status, StatusCompleted)
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("Messages: got %d, want 3", len(loaded.Messages))
	}
	for i, msg := range loaded.Messages {
		if msg.Content != legacyState.Messages[i].Content {
			t.Errorf("Message[%d].Content: got %q, want %q", i, msg.Content, legacyState.Messages[i].Content)
		}
	}

	// Verify history files count.
	histDir := filepath.Join(dir, "integrity-test", "history")
	entries, _ := os.ReadDir(histDir)
	if len(entries) != 3 {
		t.Errorf("history files: got %d, want 3", len(entries))
	}
}

func TestStore_MultipleSaves_HistoryAppend(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// First save: 2 messages.
	state := &SessionState{
		SessionID: "multi-save",
		Status:    StatusActive,
		Messages: []Message{
			{Role: "user", Content: "msg1"},
			{Role: "assistant", Content: "msg2"},
		},
	}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Second save: 3 messages (1 new).
	state.Messages = append(state.Messages, Message{Role: "user", Content: "msg3"})
	if err := store.Save(state); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	// Verify history has 3 files (not 5).
	histDir := filepath.Join(dir, "multi-save", "history")
	entries, _ := os.ReadDir(histDir)
	if len(entries) != 3 {
		t.Errorf("history files: got %d, want 3 (no duplicates)", len(entries))
	}
}

func TestStore_Cleanup_FolderMode(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create an old folder-based session.
	oldState := &SessionState{SessionID: "old-folder", Status: StatusActive}
	store.Save(oldState)
	oldMetaPath := filepath.Join(dir, "old-folder", "metadata.json")
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldMetaPath, oldTime, oldTime)

	// Create a new folder-based session.
	newState := &SessionState{SessionID: "new-folder", Status: StatusActive}
	store.Save(newState)

	removed, err := store.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed: got %d, want 1", removed)
	}

	// Old folder should be gone.
	if _, err := os.Stat(filepath.Join(dir, "old-folder")); !os.IsNotExist(err) {
		t.Error("old folder should have been removed")
	}
	// New folder should remain.
	if _, err := os.Stat(filepath.Join(dir, "new-folder", "metadata.json")); err != nil {
		t.Error("new folder should still exist")
	}
}
