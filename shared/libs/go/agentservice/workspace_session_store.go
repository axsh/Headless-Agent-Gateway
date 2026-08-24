package agentservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// WorkDirSessionLister lists sessions persisted under a workspace .tern directory.
type WorkDirSessionLister interface {
	ListByWorkDir(workDir string) ([]*codingagent.SessionRecord, error)
	ListByStorageRoot(storageRoot string) ([]*codingagent.SessionRecord, error)
}

// WorkspaceSessionStore persists SessionRecord as {session_dir}/record.json
// and keeps an in-memory cache. Conversation facts live in Canonical history/.
type WorkspaceSessionStore struct {
	mem *MemorySessionStore
}

// NewWorkspaceSessionStore creates a disk-backed SessionStore.
func NewWorkspaceSessionStore() *WorkspaceSessionStore {
	return &WorkspaceSessionStore{mem: NewMemorySessionStore()}
}

var _ codingagent.SessionStore = (*WorkspaceSessionStore)(nil)
var _ WorkDirSessionLister = (*WorkspaceSessionStore)(nil)

// Create writes record.json, initializes Canonical folders, and caches the record.
func (s *WorkspaceSessionStore) Create(rec *codingagent.SessionRecord) error {
	if rec.SessionDir == "" {
		root := EffectiveStorageRoot(rec)
		if root != "" && rec.ID != "" {
			rec.SessionDir = CanonicalSessionDir(root, rec.ID)
		}
	}
	if rec.SessionDir != "" {
		if err := persistSessionRecord(rec); err != nil {
			return err
		}
		c := session.OpenCanonical(rec.SessionDir)
		if err := c.Init(rec.ID, rec.AgentName); err != nil {
			return err
		}
	}
	return s.mem.Create(rec)
}

// Get returns a cached session. Call ListByWorkDir first after a restart.
func (s *WorkspaceSessionStore) Get(id string) (*codingagent.SessionRecord, error) {
	return s.mem.Get(id)
}

// Update writes record.json and the memory cache.
func (s *WorkspaceSessionStore) Update(rec *codingagent.SessionRecord) error {
	if rec.SessionDir != "" {
		if err := persistSessionRecord(rec); err != nil {
			return err
		}
	}
	return s.mem.Update(rec)
}

// List returns cached records only.
func (s *WorkspaceSessionStore) List() ([]*codingagent.SessionRecord, error) {
	return s.mem.List()
}

// Delete removes the record from the memory cache. Disk files are kept.
func (s *WorkspaceSessionStore) Delete(id string) error {
	return s.mem.Delete(id)
}

// ListByWorkDir scans {work_dir}/.tern/*/record.json into the cache and returns them.
func (s *WorkspaceSessionStore) ListByWorkDir(workDir string) ([]*codingagent.SessionRecord, error) {
	return s.listUnderStorageRoot(workDir)
}

// ListByStorageRoot scans {storage_root}/.tern/*/record.json into the cache and returns them.
func (s *WorkspaceSessionStore) ListByStorageRoot(storageRoot string) ([]*codingagent.SessionRecord, error) {
	return s.listUnderStorageRoot(storageRoot)
}

func (s *WorkspaceSessionStore) listUnderStorageRoot(root string) ([]*codingagent.SessionRecord, error) {
	if root == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	ternDir := filepath.Join(abs, ".tern")
	entries, err := os.ReadDir(ternDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*codingagent.SessionRecord{}, nil
		}
		return nil, err
	}
	var result []*codingagent.SessionRecord
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		recPath := filepath.Join(ternDir, e.Name(), "record.json")
		data, err := os.ReadFile(recPath)
		if err != nil {
			continue
		}
		var rec codingagent.SessionRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}
		s.mem.upsert(&rec)
		got, err := s.mem.Get(rec.ID)
		if err != nil {
			continue
		}
		result = append(result, got)
	}
	return result, nil
}

func persistSessionRecord(rec *codingagent.SessionRecord) error {
	if err := os.MkdirAll(rec.SessionDir, 0755); err != nil {
		return fmt.Errorf("mkdir session_dir: %w", err)
	}
	path := filepath.Join(rec.SessionDir, "record.json")
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			return err2
		}
	}
	return nil
}
