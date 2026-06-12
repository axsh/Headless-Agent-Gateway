package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

		// Check if background execution is requested.
		background, _ := input["background"].(bool)

		if background {
			cmd := exec.CommandContext(ctx, "sh", "-c", commandLine)
			cmd.Dir = tc.WorkDir
			if err := cmd.Start(); err != nil {
				return "", fmt.Errorf("execute_command: background start failed: %w", err)
			}
			pid := cmd.Process.Pid
			tc.Tracker.TrackProcess(pid, commandLine)
			// Detach: don't wait for the process.
			go func() { _ = cmd.Wait() }()
			return fmt.Sprintf("Background process started with PID: %d", pid), nil
		}

		// Foreground execution with combined output.
		cmd := exec.CommandContext(ctx, "sh", "-c", commandLine)
		cmd.Dir = tc.WorkDir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		result := stdout.String()
		if stderr.Len() > 0 {
			result += "\nSTDERR:\n" + stderr.String()
		}
		if err != nil {
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
			if strings.Contains(err.Error(), "process already finished") ||
				strings.Contains(err.Error(), "no such process") {
				tc.Tracker.UntrackProcess(pid)
				return fmt.Sprintf("Process %d was already terminated", pid), nil
			}
			return "", fmt.Errorf("kill_process: failed to kill process %d: %w", pid, err)
		}
		tc.Tracker.UntrackProcess(pid)
		return fmt.Sprintf("Process %d killed successfully", pid), nil
	}
}
