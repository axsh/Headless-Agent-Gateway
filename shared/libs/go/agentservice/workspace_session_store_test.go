package agentservice

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestWorkspaceSessionStore_CreateWritesRecordJSON(t *testing.T) {
	workDir := t.TempDir()
	store := NewWorkspaceSessionStore()
	rec := &codingagent.SessionRecord{
		ID:        "sess-abc",
		AgentName: "claudecode",
		WorkDir:   workDir,
	}
	if err := store.Create(rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	recordPath := filepath.Join(workDir, ".tern", "sess-abc", "record.json")
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("record.json missing: %v", err)
	}
	hist := filepath.Join(workDir, ".tern", "sess-abc", "history")
	if st, err := os.Stat(hist); err != nil || !st.IsDir() {
		t.Fatalf("history dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".tern", "session.db")); !os.IsNotExist(err) {
		t.Fatalf("session.db must not exist, err=%v", err)
	}
	if rec.SessionDir != filepath.Join(workDir, ".tern", "sess-abc") {
		t.Errorf("SessionDir = %q", rec.SessionDir)
	}
}

func TestWorkspaceSessionStore_ListByWorkDirReloads(t *testing.T) {
	workDir := t.TempDir()
	storeA := NewWorkspaceSessionStore()
	rec := &codingagent.SessionRecord{
		ID:        "sess-reload",
		AgentName: "codex",
		WorkDir:   workDir,
	}
	if err := storeA.Create(rec); err != nil {
		t.Fatal(err)
	}
	storeB := NewWorkspaceSessionStore()
	got, err := storeB.ListByWorkDir(workDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != "sess-reload" || got[0].AgentName != "codex" {
		t.Errorf("record = %+v", got[0])
	}
	loaded, err := storeB.Get("sess-reload")
	if err != nil {
		t.Fatalf("Get after ListByWorkDir: %v", err)
	}
	wantDir := filepath.Join(workDir, ".tern", "sess-reload")
	if loaded.SessionDir != wantDir {
		t.Errorf("SessionDir = %q, want %q", loaded.SessionDir, wantDir)
	}
}

func TestNativeSessionDir(t *testing.T) {
	got := NativeSessionDir("/tmp/sess")
	want := filepath.Join("/tmp/sess", "native")
	if got != want {
		t.Errorf("NativeSessionDir = %q, want %q", got, want)
	}
	if NativeSessionDir("") != "" {
		t.Error("empty session dir should yield empty native dir")
	}
}
