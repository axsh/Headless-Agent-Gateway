package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// newRunBackgroundProcess creates the run_background_process tool handler.
// It starts a command as a background process and returns a structured JSON
// response containing the PID, status, and original command string.
func newRunBackgroundProcess(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		command, _ := input["command"].(string)
		if command == "" {
			return "", fmt.Errorf("run_background_process: command is required")
		}

		// Check blocked commands.
		if tc.IsBlockedCommand(command) {
			return "", fmt.Errorf("run_background_process: command blocked: %s", command)
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = tc.WorkDir
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("run_background_process: start failed: %w", err)
		}

		pid := cmd.Process.Pid
		tc.Tracker.TrackProcess(pid, command)

		// Detach: don't wait for the process.
		go func() { _ = cmd.Wait() }()

		// Return structured JSON response for predictable PID extraction.
		result, _ := json.Marshal(map[string]any{
			"status":  "started",
			"pid":     pid,
			"command": command,
		})
		return string(result), nil
	}
}
