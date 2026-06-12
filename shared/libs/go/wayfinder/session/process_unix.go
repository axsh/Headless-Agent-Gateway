//go:build !windows

package session

import (
	"os"
	"syscall"
)

// verifyProcessAlive checks if a process with the given PID is still alive.
func verifyProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to actually check.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
