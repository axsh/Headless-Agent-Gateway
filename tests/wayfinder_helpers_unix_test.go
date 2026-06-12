//go:build !windows

package llm_test

import "syscall"

// isProcessAlive checks if a process with the given PID is still running.
// On Unix, this uses signal 0 which doesn't actually send a signal
// but checks if the process exists.
func isProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
