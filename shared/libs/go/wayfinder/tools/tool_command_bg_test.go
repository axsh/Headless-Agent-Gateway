package tools

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"
)

func TestRunBackgroundProcess_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("background process test requires sh")
	}

	workDir := t.TempDir()
	tc := testContext(workDir)
	handler := newRunBackgroundProcess(tc)

	result, err := handler(context.Background(), map[string]any{"command": "sleep 30"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parse structured JSON response.
	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v\nraw: %s", err, result)
	}

	// Verify required fields.
	if resp["status"] != "started" {
		t.Errorf("status = %v, want %q", resp["status"], "started")
	}
	pid, ok := resp["pid"].(float64)
	if !ok || pid <= 0 {
		t.Errorf("pid should be a positive number, got %v", resp["pid"])
	}
	if resp["command"] != "sleep 30" {
		t.Errorf("command = %v, want %q", resp["command"], "sleep 30")
	}

	// Cleanup: kill the background process.
	if pid > 0 {
		tc.Tracker.UntrackProcess(int(pid))
	}
}

func TestRunBackgroundProcess_BlockedCommand(t *testing.T) {
	workDir := t.TempDir()
	tc := testContextWithBlocker(workDir)
	handler := newRunBackgroundProcess(tc)

	_, err := handler(context.Background(), map[string]any{"command": "sudo rm -rf /"})
	if err == nil {
		t.Fatal("expected error for blocked command")
	}
}

func TestRunBackgroundProcess_EmptyCommand(t *testing.T) {
	tc := testContext(t.TempDir())
	handler := newRunBackgroundProcess(tc)

	_, err := handler(context.Background(), map[string]any{"command": ""})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}
