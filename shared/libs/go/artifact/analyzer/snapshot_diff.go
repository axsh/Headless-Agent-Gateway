// Snapshot diff assists reconciliation when workDir is not a git repository.
//
// Limitation: concurrent changes outside the agent session may appear in the diff.
package analyzer

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// DirSnapshot captures file paths and sizes at a point in time.
type DirSnapshot struct {
	Files map[string]int64 // relative path → size
}

// TakeSnapshot walks workDir (skipping .git) and records relative paths.
func TakeSnapshot(workDir string) (DirSnapshot, error) {
	snap := DirSnapshot{Files: make(map[string]int64)}
	err := filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(workDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		snap.Files[rel] = info.Size()
		return nil
	})
	return snap, err
}

// DiffSnapshots returns create/update/delete ops between before and after.
func DiffSnapshots(before, after DirSnapshot) []ParsedFileOp {
	seen := make(map[string]string)

	for path, size := range after.Files {
		prev, ok := before.Files[path]
		if !ok {
			seen[path] = store.OperationCreate
			continue
		}
		if prev != size {
			seen[path] = store.OperationUpdate
		}
	}
	for path := range before.Files {
		if _, ok := after.Files[path]; !ok {
			seen[path] = store.OperationDelete
		}
	}

	out := make([]ParsedFileOp, 0, len(seen))
	for path, op := range seen {
		out = append(out, ParsedFileOp{Path: path, Operation: op})
	}
	return out
}

// SnapshotExists reports whether a snapshot captured any files.
func (s DirSnapshot) SnapshotExists() bool {
	return len(s.Files) > 0 || s.Files != nil
}

// EmptySnapshot returns an empty snapshot marker (used when snapshot was taken on empty dir).
func EmptySnapshot() DirSnapshot {
	return DirSnapshot{Files: make(map[string]int64)}
}

// HasSnapshotData returns true if TakeSnapshot was invoked (Files map non-nil).
func HasSnapshotData(s DirSnapshot) bool {
	return s.Files != nil
}

// WriteFile is a test helper to create files under workDir.
func WriteFile(workDir, relPath, content string) error {
	full := filepath.Join(workDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}
