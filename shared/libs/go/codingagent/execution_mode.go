package codingagent

const (
	// ExecutionModeInteractive keeps stdin open for mid-task user responses.
	ExecutionModeInteractive = "interactive"
	// ExecutionModeSingleShot closes stdin after the initial prompt (legacy behavior).
	ExecutionModeSingleShot = "single_shot"
)

// NormalizeExecutionMode returns a valid execution mode, defaulting to interactive.
func NormalizeExecutionMode(mode string) string {
	switch mode {
	case ExecutionModeSingleShot:
		return ExecutionModeSingleShot
	default:
		return ExecutionModeInteractive
	}
}
