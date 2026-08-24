package agentservice

import (
	"path/filepath"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ResolveStorageRoot returns storageRoot when non-empty, otherwise workDir.
func ResolveStorageRoot(storageRoot, workDir string) string {
	if storageRoot != "" {
		return storageRoot
	}
	return workDir
}

// WayfinderDir returns the Wayfinder/Tern Home: {storageRoot}/.tern (no session_id).
func WayfinderDir(storageRoot string) string {
	return filepath.Join(storageRoot, ".tern")
}

// CanonicalSessionDir returns the Tern canonical leaf: {storageRoot}/.tern/{sessionID}.
func CanonicalSessionDir(storageRoot, sessionID string) string {
	return filepath.Join(WayfinderDir(storageRoot), sessionID)
}

// EffectiveStorageRoot returns record.StorageRoot, or WorkDir when empty (legacy records).
func EffectiveStorageRoot(rec *codingagent.SessionRecord) string {
	if rec == nil {
		return ""
	}
	if rec.StorageRoot != "" {
		return rec.StorageRoot
	}
	return rec.WorkDir
}
