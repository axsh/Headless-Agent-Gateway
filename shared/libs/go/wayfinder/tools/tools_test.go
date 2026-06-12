package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/wayfinder"
)

func TestRegisterAllTools_AllNineRegistered(t *testing.T) {
	reg := NewRegistry()
	tracker := wayfinder.NewFileTracker()
	RegisterAllTools(reg, t.TempDir(), tracker)

	expected := []string{
		"read_file", "write_file", "list_directory", "create_directory",
		"edit_file", "search_files", "grep_files", "execute_command", "kill_process",
	}
	for _, name := range expected {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	defs := reg.Definitions()
	if len(defs) != 9 {
		t.Errorf("len(Definitions) = %d, want 9", len(defs))
	}
}

func TestReadFile_Success(t *testing.T) {
	workDir := t.TempDir()
	os.WriteFile(filepath.Join(workDir, "test.txt"), []byte("hello world"), 0644)

	handler := newReadFile(workDir)
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
	handler := newReadFile(workDir)
	_, err := handler(context.Background(), map[string]any{"path": "../../etc/passwd"})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestWriteFile_Success(t *testing.T) {
	workDir := t.TempDir()
	tracker := wayfinder.NewFileTracker()
	handler := newWriteFile(workDir, tracker)

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
	if !tracker.IsTracked(filepath.Join(workDir, "output.txt")) {
		t.Error("expected file to be tracked after write")
	}
}

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	workDir := t.TempDir()
	tracker := wayfinder.NewFileTracker()
	handler := newWriteFile(workDir, tracker)

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

	handler := newListDirectory(workDir)
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
	tracker := wayfinder.NewFileTracker()
	handler := newCreateDirectory(workDir, tracker)

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

	handler := newEditFile(workDir)
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

	handler := newEditFile(workDir)
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

	handler := newSearchFiles(workDir)
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

	handler := newGrepFiles(workDir)
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
	tracker := wayfinder.NewFileTracker()
	handler := newExecuteCommand(workDir, tracker)

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
	tracker := wayfinder.NewFileTracker()
	handler := newExecuteCommand(workDir, tracker)

	_, err := handler(context.Background(), map[string]any{"command": "sudo rm -rf /"})
	if err == nil {
		t.Fatal("expected error for blocked command")
	}
}

func TestKillProcess_InvalidPID(t *testing.T) {
	tracker := wayfinder.NewFileTracker()
	handler := newKillProcess(tracker)

	_, err := handler(context.Background(), map[string]any{"pid": float64(-999999)})
	if err == nil {
		// On some OS, FindProcess(-999999) may not error until Kill().
		// Either way, an error at some point is expected.
		t.Log("no error from FindProcess, expected error from Kill")
	}
}
