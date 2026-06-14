package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store manages session state persistence on the filesystem.
// Sessions are stored as folder structures:
//
//	{rootDir}/{sessionID}/metadata.json   -- session metadata
//	{rootDir}/{sessionID}/context.json    -- current LLM context messages
//	{rootDir}/{sessionID}/history/NNN.json -- individual message history
type Store struct {
	rootDir string
}

// NewStore creates a new Store for the given root directory.
func NewStore(rootDir string) *Store {
	return &Store{rootDir: rootDir}
}

// sessionDir returns the directory path for a session.
func (s *Store) sessionDir(sessionID string) string {
	return filepath.Join(s.rootDir, sessionID)
}

// Load reads session state from folder structure.
// Falls back to legacy single-file format with auto-migration.
func (s *Store) Load(sessionID string) (*SessionState, error) {
	dir := s.sessionDir(sessionID)
	metaPath := filepath.Join(dir, "metadata.json")

	// Try new folder format first.
	if _, err := os.Stat(metaPath); err == nil {
		return s.loadFromFolder(sessionID)
	}

	// Try legacy single-file format.
	legacyPath := filepath.Join(s.rootDir, sessionID+".json")
	data, err := os.ReadFile(legacyPath)
	if os.IsNotExist(err) {
		return nil, nil // New session.
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}

	// Auto-migrate legacy format to folder structure.
	if err := s.migrateToFolder(&state); err != nil {
		// Migration failed; return legacy data without failing.
		return &state, nil
	}

	return &state, nil
}

// Save writes session state to the folder structure.
func (s *Store) Save(state *SessionState) error {
	dir := s.sessionDir(state.SessionID)
	histDir := filepath.Join(dir, "history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		return fmt.Errorf("failed to create session dir: %w", err)
	}

	// Determine how many new messages to append to history.
	prevLatest := 0
	metaPath := filepath.Join(dir, "metadata.json")
	if data, err := os.ReadFile(metaPath); err == nil {
		var prevMeta SessionMetadata
		if json.Unmarshal(data, &prevMeta) == nil {
			prevLatest = prevMeta.Latest
		}
	}

	// Append new messages to history.
	// Messages beyond prevLatest count are considered new.
	newMsgCount := len(state.Messages) - prevLatest
	if newMsgCount > 0 && newMsgCount <= len(state.Messages) {
		newMsgs := state.Messages[len(state.Messages)-newMsgCount:]
		if err := AppendHistory(histDir, newMsgs, prevLatest); err != nil {
			return fmt.Errorf("failed to append history: %w", err)
		}
	}

	// Build and save metadata.json.
	now := time.Now()
	state.LastActivityAt = now
	meta := SessionMetadata{
		SessionID:        state.SessionID,
		ParentID:         state.ParentID,
		Status:           state.Status,
		Latest:           len(state.Messages),
		ContextStart:     0, // Updated by compaction.
		CreatedAt:        state.CreatedAt,
		UpdatedAt:        now,
		WBSTreeJSON:      state.WBSTreeJSON,
		CreatedFiles:     state.CreatedFiles,
		RunningProcesses: state.RunningProcesses,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := atomicWrite(metaPath, metaData); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Save context.json (current LLM messages).
	ctxPath := filepath.Join(dir, "context.json")
	ctxData, err := json.MarshalIndent(state.Messages, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}
	if err := atomicWrite(ctxPath, ctxData); err != nil {
		return fmt.Errorf("failed to write context: %w", err)
	}

	return nil
}

// loadFromFolder loads session state from the new folder structure.
func (s *Store) loadFromFolder(sessionID string) (*SessionState, error) {
	dir := s.sessionDir(sessionID)

	// Read metadata.json.
	metaData, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}
	var meta SessionMetadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Read context.json (current messages).
	var msgs []Message
	ctxData, err := os.ReadFile(filepath.Join(dir, "context.json"))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read context: %w", err)
		}
		// context.json missing; messages will be empty.
	} else {
		if err := json.Unmarshal(ctxData, &msgs); err != nil {
			return nil, fmt.Errorf("failed to parse context: %w", err)
		}
	}

	// Reconstruct SessionState.
	state := &SessionState{
		SessionID:        meta.SessionID,
		ParentID:         meta.ParentID,
		Status:           meta.Status,
		Messages:         msgs,
		CreatedFiles:     meta.CreatedFiles,
		RunningProcesses: meta.RunningProcesses,
		WBSTreeJSON:      meta.WBSTreeJSON,
		CreatedAt:        meta.CreatedAt,
		LastActivityAt:   meta.UpdatedAt,
	}
	return state, nil
}

// migrateToFolder converts a legacy single-file session to folder format.
func (s *Store) migrateToFolder(state *SessionState) error {
	dir := s.sessionDir(state.SessionID)
	histDir := filepath.Join(dir, "history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		return fmt.Errorf("migrate: create dir: %w", err)
	}

	// Write all messages to history/.
	if err := AppendHistory(histDir, state.Messages, 0); err != nil {
		return fmt.Errorf("migrate: append history: %w", err)
	}

	// Write metadata.json.
	now := time.Now()
	meta := SessionMetadata{
		SessionID:        state.SessionID,
		ParentID:         state.ParentID,
		Status:           state.Status,
		Latest:           len(state.Messages),
		ContextStart:     0,
		CreatedAt:        state.CreatedAt,
		UpdatedAt:        now,
		WBSTreeJSON:      state.WBSTreeJSON,
		CreatedFiles:     state.CreatedFiles,
		RunningProcesses: state.RunningProcesses,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("migrate: marshal metadata: %w", err)
	}
	if err := atomicWrite(filepath.Join(dir, "metadata.json"), metaData); err != nil {
		return fmt.Errorf("migrate: write metadata: %w", err)
	}

	// Write context.json.
	ctxData, err := json.MarshalIndent(state.Messages, "", "  ")
	if err != nil {
		return fmt.Errorf("migrate: marshal context: %w", err)
	}
	if err := atomicWrite(filepath.Join(dir, "context.json"), ctxData); err != nil {
		return fmt.Errorf("migrate: write context: %w", err)
	}

	// Rename legacy file to .bak.
	legacyPath := filepath.Join(s.rootDir, state.SessionID+".json")
	backupPath := legacyPath + ".bak"
	os.Rename(legacyPath, backupPath) // Best-effort; ignore errors.

	return nil
}

// Cleanup removes session folders/files older than the threshold.
func (s *Store) Cleanup(threshold time.Duration) (int, error) {
	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		return 0, err
	}
	removed := 0
	cutoff := time.Now().Add(-threshold)
	for _, e := range entries {
		entryPath := filepath.Join(s.rootDir, e.Name())

		if e.IsDir() {
			// Folder-based session: check metadata.json modification time.
			metaPath := filepath.Join(entryPath, "metadata.json")
			info, err := os.Stat(metaPath)
			if err != nil {
				continue // Not a session folder.
			}
			if info.ModTime().Before(cutoff) {
				os.RemoveAll(entryPath)
				removed++
			}
		} else if filepath.Ext(e.Name()) == ".json" {
			// Legacy single-file session.
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				os.Remove(entryPath)
				removed++
			}
		}
	}
	return removed, nil
}

// LoadHistory reads messages from a session's history/ folder.
func (s *Store) LoadHistory(sessionID string, fromSeq, toSeq int) ([]Message, error) {
	histDir := filepath.Join(s.sessionDir(sessionID), "history")
	return LoadHistory(histDir, fromSeq, toSeq)
}

// atomicWrite writes data to a temp file then renames to target.
func atomicWrite(targetPath string, data []byte) error {
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}
