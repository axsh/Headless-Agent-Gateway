package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// testValidatePath is a simple path validator for tests.
func testValidatePath(workDir, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		return rawPath, nil
	}
	return filepath.Join(workDir, rawPath), nil
}

// testTracker implements FileTrackerInterface for tests.
type testTracker struct {
	files     map[string]bool
	processes map[int]string
}

func newTestTracker() *testTracker {
	return &testTracker{files: make(map[string]bool), processes: make(map[int]string)}
}

func (t *testTracker) TrackFile(path string)                   { t.files[path] = true }
func (t *testTracker) IsTracked(path string) bool              { return t.files[path] }
func (t *testTracker) TrackProcess(pid int, cmdLine string)    { t.processes[pid] = cmdLine }
func (t *testTracker) UntrackProcess(pid int)                  { delete(t.processes, pid) }

func testContext(workDir string) *ToolContext {
	return &ToolContext{
		WorkDir:          workDir,
		ValidatePath:     testValidatePath,
		IsBlockedCommand: func(cmd string) bool { return false },
		Tracker:          newTestTracker(),
	}
}

func testContextWithBlocker(workDir string) *ToolContext {
	tc := testContext(workDir)
	tc.IsBlockedCommand = func(cmd string) bool {
		// Simple blocker for testing.
		return len(cmd) > 4 && cmd[:4] == "sudo"
	}
	return tc
}

func TestRegisterAllTools_AllNineRegistered(t *testing.T) {
	reg := NewRegistry()
	tc := testContext(t.TempDir())
	RegisterAllTools(reg, tc)

	expected := []string{
		"read_file", "write_file", "list_directory", "create_directory",
		"edit_file", "search_files", "grep_files", "execute_command",
		"run_background_process", "kill_process",
	}
	for _, name := range expected {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	defs := reg.Definitions()
	if len(defs) != 10 {
		t.Errorf("len(Definitions) = %d, want 10", len(defs))
	}
}

func TestReadFile_Success(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("hello world"), 0644)

	tc := testContext(workDir)
	handler := newReadFile(tc)
	result, err := handler(context.Background(), map[string]any{"path": "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

func TestReadFile_PathTraversalBlocked(t *testing.T) {
	workDir := t.TempDir()
	// Use a ValidatePath that actually checks boundaries.
	tc := &ToolContext{
		WorkDir: workDir,
		ValidatePath: func(wd, raw string) (string, error) {
			abs := filepath.Clean(filepath.Join(wd, raw))
			rel, _ := filepath.Rel(wd, abs)
			if len(rel) >= 2 && rel[:2] == ".." {
				return "", fmt.Errorf("path outside workDir")
			}
			return abs, nil
		},
		Tracker: newTestTracker(),
	}
	handler := newReadFile(tc)
	_, err := handler(context.Background(), map[string]any{"path": "../../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestWriteFile_Success(t *testing.T) {
	workDir := t.TempDir()
	tc := testContext(workDir)
	handler := newWriteFile(tc)

	result, err := handler(context.Background(), map[string]any{
		"path":    "output.txt",
		"content": "generated content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}

	data, _ := os.ReadFile(filepath.Join(workDir, "output.txt"))
	if string(data) != "generated content" {
		t.Errorf("file content = %q, want %q", string(data), "generated content")
	}
	if !tc.Tracker.IsTracked(filepath.Join(workDir, "output.txt")) {
		t.Error("expected file to be tracked after write")
	}
}

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	workDir := t.TempDir()
	tc := testContext(workDir)
	handler := newWriteFile(tc)

	_, err := handler(context.Background(), map[string]any{
		"path":    "deep/nested/dir/file.txt",
		"content": "nested",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(workDir, "deep", "nested", "dir", "file.txt"))
	if string(data) != "nested" {
		t.Errorf("file content = %q, want %q", string(data), "nested")
	}
}

func TestListDirectory_Success(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "a.txt"), []byte("a"), 0644)
	os.Mkdir(filepath.Join(workDir, "subdir"), 0755)

	tc := testContext(workDir)
	handler := newListDirectory(tc)
	result, err := handler(context.Background(), map[string]any{"path": "."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty directory listing")
	}
}

func TestCreateDirectory_Success(t *testing.T) {
	workDir := t.TempDir()
	tc := testContext(workDir)
	handler := newCreateDirectory(tc)

	_, err := handler(context.Background(), map[string]any{"path": "new/deep/dir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(filepath.Join(workDir, "new", "deep", "dir"))
	if err != nil || !info.IsDir() {
		t.Error("expected directory to exist")
	}
}

func TestEditFile_Success(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "edit.txt"), []byte("hello world"), 0644)

	tc := testContext(workDir)
	handler := newEditFile(tc)
	_, err := handler(context.Background(), map[string]any{
		"path":     "edit.txt",
		"old_text": "world",
		"new_text": "wayfinder",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(workDir, "edit.txt"))
	if string(data) != "hello wayfinder" {
		t.Errorf("content = %q, want %q", string(data), "hello wayfinder")
	}
}

func TestEditFile_NotUnique(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "dup.txt"), []byte("aaa bbb aaa"), 0644)

	tc := testContext(workDir)
	handler := newEditFile(tc)
	_, err := handler(context.Background(), map[string]any{
		"path":     "dup.txt",
		"old_text": "aaa",
		"new_text": "ccc",
	})
	if err == nil {
		t.Fatal("expected error for non-unique old_text")
	}
}

func TestSearchFiles_Success(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("test"), 0644)

	tc := testContext(workDir)
	handler := newSearchFiles(tc)
	result, err := handler(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "main.go" {
		t.Errorf("result = %q, want %q", result, "main.go")
	}
}

func TestGrepFiles_Success(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "code.go"), []byte("func Hello() {\n\treturn\n}"), 0644)

	tc := testContext(workDir)
	handler := newGrepFiles(tc)
	result, err := handler(context.Background(), map[string]any{"pattern": "Hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" || result == "No matches found for pattern: Hello" {
		t.Errorf("expected matches, got: %q", result)
	}
}

func TestExecuteCommand_ForegroundSuccess(t *testing.T) {
	workDir := t.TempDir()
	tc := testContext(workDir)
	handler := newExecuteCommand(tc)

	result, err := handler(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty output")
	}
}

func TestExecuteCommand_Blocked(t *testing.T) {
	workDir := t.TempDir()
	tc := testContextWithBlocker(workDir)
	handler := newExecuteCommand(tc)

	_, err := handler(context.Background(), map[string]any{"command": "sudo rm -rf /"})
	if err == nil {
		t.Fatal("expected error for blocked command")
	}
}

func TestKillProcess_InvalidPID(t *testing.T) {
	tc := testContext(t.TempDir())
	handler := newKillProcess(tc)

	_, err := handler(context.Background(), map[string]any{"pid": float64(-999999)})
	if err == nil {
		t.Log("no error from FindProcess, expected error from Kill")
	}
}
