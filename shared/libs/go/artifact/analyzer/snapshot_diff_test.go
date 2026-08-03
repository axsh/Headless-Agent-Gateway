package analyzer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffSnapshots_CreateUpdateDelete(t *testing.T) {
	before := analyzer.DirSnapshot{Files: map[string]int64{
		"old.txt":  3,
		"gone.txt": 1,
	}}
	after := analyzer.DirSnapshot{Files: map[string]int64{
		"old.txt":  5,
		"new.txt":  2,
	}}

	ops := analyzer.DiffSnapshots(before, after)
	assert.Len(t, ops, 3)

	found := map[string]string{}
	for _, op := range ops {
		found[op.Path] = op.Operation
	}
	assert.Equal(t, store.OperationUpdate, found["old.txt"])
	assert.Equal(t, store.OperationCreate, found["new.txt"])
	assert.Equal(t, store.OperationDelete, found["gone.txt"])
}

func TestTakeSnapshot_SkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, analyzer.WriteFile(dir, "visible.txt", "x"))

	snap, err := analyzer.TakeSnapshot(dir)
	require.NoError(t, err)
	_, hasGit := snap.Files[".git"]
	assert.False(t, hasGit)
	assert.Contains(t, snap.Files, "visible.txt")
}
