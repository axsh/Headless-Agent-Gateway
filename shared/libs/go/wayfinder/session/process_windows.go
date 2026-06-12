//go:build windows

package session

import (
	"os/exec"
	"strconv"
	"strings"
)

// verifyProcessAlive checks if a process with the given PID exists on Windows.
func verifyProcessAlive(pid int) bool {
	// Use tasklist to check if the process exists.
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// tasklist output contains the PID if the process exists.
	return strings.Contains(string(output), strconv.Itoa(pid))
}
