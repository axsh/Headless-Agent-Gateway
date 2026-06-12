package session

import "os"

// ValidateTrackerState verifies integrity of deserialized tracker data.
// Removes entries that no longer match actual system state.
func ValidateTrackerState(state *SessionState) {
	// 1. Validate files/directories exist.
	validFiles := make([]TrackedFile, 0, len(state.CreatedFiles))
	for _, f := range state.CreatedFiles {
		if _, err := os.Stat(f.Path); err == nil {
			validFiles = append(validFiles, f)
		}
		// Non-existent entries are excluded from deletion permission list.
	}
	state.CreatedFiles = validFiles

	// 2. Validate process existence.
	validProcs := make([]TrackedProcess, 0, len(state.RunningProcesses))
	for _, p := range state.RunningProcesses {
		if verifyProcessAlive(p.PID) {
			validProcs = append(validProcs, p)
		}
		// Dead or reassigned processes are excluded.
	}
	state.RunningProcesses = validProcs
}
