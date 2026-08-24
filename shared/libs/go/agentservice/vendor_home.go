package agentservice

import "path/filepath"

// VendorHomeDir returns the Coding Agent home directory for launch/overlay.
// Mapping:
//
//	codex       → {storageRoot}/.codex
//	claudecode  → {storageRoot}/.claude   // never {storageRoot}/.claudecode
//	wayfinder   → sessionDir              // Tern canonical leaf (.tern/{id}); NOT .wayfinder, NOT .../native
//
// Empty inputs that are required for the agent return "".
func VendorHomeDir(storageRoot, agentName, sessionDir string) string {
	switch agentName {
	case "codex":
		if storageRoot == "" {
			return ""
		}
		return filepath.Join(storageRoot, ".codex")
	case "claudecode":
		if storageRoot == "" {
			return ""
		}
		return filepath.Join(storageRoot, ".claude")
	case "wayfinder":
		return sessionDir
	case "":
		return ""
	default:
		if storageRoot == "" {
			return ""
		}
		return filepath.Join(storageRoot, "."+agentName)
	}
}
