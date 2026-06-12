package session

import (
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

	// Check that no .tmp file remains.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file found: %s", e.Name())
		}
	}

	// Check that the real file exists.
	if _, err := os.Stat(filepath.Join(dir, "atomic-test.json")); err != nil {
		t.Errorf("session file should exist: %v", err)
	}
}

func TestStore_Cleanup(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Create an old file with modified time in the past.
	oldFile := filepath.Join(dir, "old-session.json")
	os.WriteFile(oldFile, []byte(`{}`), 0644)
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldFile, oldTime, oldTime)

	// Create a new file.
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
	// New file should still exist.
	if _, err := os.Stat(filepath.Join(dir, "new-session.json")); err != nil {
		t.Error("new file should still exist")
	}
}

func TestStore_SaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "session", "dir")
	store := NewStore(dir)

	state := &SessionState{SessionID: "auto-dir", Status: StatusActive}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save should create dir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "auto-dir.json")); err != nil {
		t.Errorf("session file should exist after auto-dir creation: %v", err)
	}
}
