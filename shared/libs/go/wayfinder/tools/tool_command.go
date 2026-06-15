package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// newExecuteCommand creates the execute_command tool handler.
func newExecuteCommand(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		commandLine, _ := input["command"].(string)
		if commandLine == "" {
			return "", fmt.Errorf("execute_command: command is required")
		}

		// Check blocked commands.
		if tc.IsBlockedCommand(commandLine) {
			return "", fmt.Errorf("execute_command: command is blocked for safety: %s", commandLine)
		}

		// Determine timeout (default: 120 seconds).
		timeout := 120 * time.Second
		if t, ok := input["timeout_seconds"].(float64); ok && t > 0 {
			timeout = time.Duration(t) * time.Second
		}

		// Create timeout context.
		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Foreground execution with combined output.
		cmd := exec.CommandContext(execCtx, "sh", "-c", commandLine)
		cmd.Dir = tc.WorkDir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		result := stdout.String()
		if stderr.Len() > 0 {
			result += "\nSTDERR:\n" + stderr.String()
		}
		if execCtx.Err() == context.DeadlineExceeded {
			result += fmt.Sprintf("\nCommand timed out after %d seconds", int(timeout.Seconds()))
		} else if err != nil {
			result += fmt.Sprintf("\nExit error: %v", err)
		}

		// Truncate output to prevent context overflow.
		const maxOutputLen = 100000
		if len(result) > maxOutputLen {
			result = result[:maxOutputLen] + "\n... (output truncated)"
		}

		return result, nil
	}
}

// newKillProcess creates the kill_process tool handler.
func newKillProcess(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		pidRaw, ok := input["pid"]
		if !ok {
			return "", fmt.Errorf("kill_process: pid is required")
		}

		var pid int
		switch v := pidRaw.(type) {
		case float64:
			pid = int(v)
		case string:
			var err error
			pid, err = strconv.Atoi(v)
			if err != nil {
				return "", fmt.Errorf("kill_process: invalid pid %q: %w", v, err)
			}
		default:
			return "", fmt.Errorf("kill_process: invalid pid type %T", pidRaw)
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return "", fmt.Errorf("kill_process: process %d not found: %w", pid, err)
		}
		if err := proc.Kill(); err != nil {
			// Handle cases where the process is already gone.
			if strings.Contains(err.Error(), "process already finished") ||
				strings.Contains(err.Error(), "no such process") ||
				strings.Contains(err.Error(), "Access is denied") {
				tc.Tracker.UntrackProcess(pid)
				result, _ := json.Marshal(map[string]any{
					"status": "already_terminated",
					"pid":    pid,
				})
				return string(result), nil
			}
			return "", fmt.Errorf("kill_process: failed to kill process %d: %w", pid, err)
		}
		tc.Tracker.UntrackProcess(pid)
		result, _ := json.Marshal(map[string]any{
			"status": "killed",
			"pid":    pid,
		})
		return string(result), nil
	}
}
