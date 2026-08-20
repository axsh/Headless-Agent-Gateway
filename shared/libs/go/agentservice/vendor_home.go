package agentservice

import "path/filepath"

// VendorHomeDir returns the Coding Agent home directory for launch/overlay.
// Mapping:
//
//	codex       → {workDir}/.codex
//	claudecode  → {workDir}/.claude   // never {workDir}/.claudecode
//	wayfinder   → sessionDir          // Tern canonical root (.tern/{id}); NOT .wayfinder, NOT .../native
//
// Empty inputs that are required for the agent return "".
func VendorHomeDir(workDir, agentName, sessionDir string) string {
	switch agentName {
	case "codex":
		if workDir == "" {
			return ""
		}
		return filepath.Join(workDir, ".codex")
	case "claudecode":
		if workDir == "" {
			return ""
		}
		return filepath.Join(workDir, ".claude")
	case "wayfinder":
		return sessionDir
	case "":
		return ""
	default:
		if workDir == "" {
			return ""
		}
		return filepath.Join(workDir, "."+agentName)
	}
}
