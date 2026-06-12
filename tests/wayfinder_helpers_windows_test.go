//go:build windows

package llm_test

import (
	"os/exec"
	"strconv"
	"strings"
)

// isProcessAlive checks if a process with the given PID is still running.
// On Windows, this uses tasklist to query the process.
func isProcessAlive(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// tasklist output for a running process contains the PID.
	// If the process is not found, it says "No tasks are running..."
	output := strings.TrimSpace(string(out))
	return strings.Contains(output, strconv.Itoa(pid))
}
