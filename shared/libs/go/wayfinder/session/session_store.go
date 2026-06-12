package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Store manages session state persistence on the filesystem.
type Store struct {
	sessionDir string
}

// NewStore creates a new Store for the given session directory.
func NewStore(sessionDir string) *Store {
	return &Store{sessionDir: sessionDir}
}

// Load reads a session state from [sessionDir]/[sessionID].json.
// Returns nil, nil if the file does not exist (new session).
func (s *Store) Load(sessionID string) (*SessionState, error) {
	path := s.filePath(sessionID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}
	return &state, nil
}

// Save writes session state atomically.
// Writes to a temp file first, then renames to avoid corruption.
func (s *Store) Save(state *SessionState) error {
	if err := os.MkdirAll(s.sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session dir: %w", err)
	}
	state.LastActivityAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}
	return atomicWrite(s.filePath(state.SessionID), data)
}

// Cleanup removes session files older than the threshold.
func (s *Store) Cleanup(threshold time.Duration) (int, error) {
	entries, err := os.ReadDir(s.sessionDir)
	if err != nil {
		return 0, err
	}
	removed := 0
	cutoff := time.Now().Add(-threshold)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(s.sessionDir, e.Name()))
			removed++
		}
	}
	return removed, nil
}

// filePath returns the full path for a session file.
func (s *Store) filePath(sessionID string) string {
	return filepath.Join(s.sessionDir, sessionID+".json")
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
