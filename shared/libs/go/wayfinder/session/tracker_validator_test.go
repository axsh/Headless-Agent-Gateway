package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestValidateTrackerState_FileExists(t *testing.T) {
	tmpDir := t.TempDir()
	existingFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(existingFile, []byte("data"), 0644)

	state := &SessionState{
		CreatedFiles: []TrackedFile{
			{Path: existingFile, CreatedAt: time.Now()},
		},
	}
	ValidateTrackerState(state)
	if len(state.CreatedFiles) != 1 {
		t.Errorf("len(CreatedFiles) = %d, want 1 (existing file should be preserved)", len(state.CreatedFiles))
	}
}

func TestValidateTrackerState_FileRemoved(t *testing.T) {
	state := &SessionState{
		CreatedFiles: []TrackedFile{
			{Path: "/nonexistent/path/file.txt", CreatedAt: time.Now()},
		},
	}
	ValidateTrackerState(state)
	if len(state.CreatedFiles) != 0 {
		t.Errorf("len(CreatedFiles) = %d, want 0 (missing file should be removed)", len(state.CreatedFiles))
	}
}

func TestValidateTrackerState_ProcessAlive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process validation test uses unix sleep command")
	}

	// Start a real background process.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep process: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	state := &SessionState{
		RunningProcesses: []TrackedProcess{
			{PID: cmd.Process.Pid, Command: "sleep", StartedAt: time.Now()},
		},
	}
	ValidateTrackerState(state)
	if len(state.RunningProcesses) != 1 {
		t.Errorf("len(RunningProcesses) = %d, want 1 (alive process should be preserved)", len(state.RunningProcesses))
	}
}

func TestValidateTrackerState_ProcessDead(t *testing.T) {
	state := &SessionState{
		RunningProcesses: []TrackedProcess{
			{PID: 999999999, Command: "nonexistent_process", StartedAt: time.Now()},
		},
	}
	ValidateTrackerState(state)
	if len(state.RunningProcesses) != 0 {
		t.Errorf("len(RunningProcesses) = %d, want 0 (dead process should be removed)", len(state.RunningProcesses))
	}
}

func TestValidateTrackerState_EmptyState(t *testing.T) {
	state := &SessionState{
		CreatedFiles:     []TrackedFile{},
		RunningProcesses: []TrackedProcess{},
	}
	ValidateTrackerState(state)
	if len(state.CreatedFiles) != 0 {
		t.Errorf("len(CreatedFiles) = %d, want 0", len(state.CreatedFiles))
	}
	if len(state.RunningProcesses) != 0 {
		t.Errorf("len(RunningProcesses) = %d, want 0", len(state.RunningProcesses))
	}
}
