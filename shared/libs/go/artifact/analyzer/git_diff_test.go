package analyzer_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	requireGit(t)
	require.NoError(t, exec.Command("git", "-C", dir, "init").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "config", "user.name", "test").Run())
}

func TestDetectGitChanges_NotARepo(t *testing.T) {
	dir := t.TempDir()
	changes, err := analyzer.DetectGitChanges(dir)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestDetectGitChanges_NewUntrackedFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	require.NoError(t, analyzer.WriteFile(dir, "new.txt", "hello"))

	changes, err := analyzer.DetectGitChanges(dir)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "new.txt", changes[0].Path)
	assert.Equal(t, store.OperationCreate, changes[0].Operation)
}

func TestDetectGitChanges_ModifiedTrackedFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	require.NoError(t, analyzer.WriteFile(dir, "tracked.txt", "v1"))
	require.NoError(t, exec.Command("git", "-C", dir, "add", "tracked.txt").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "commit", "-m", "init").Run())
	require.NoError(t, analyzer.WriteFile(dir, "tracked.txt", "v2"))

	changes, err := analyzer.DetectGitChanges(dir)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, store.OperationUpdate, changes[0].Operation)
}

func TestDetectGitChanges_GitignoreExcluded(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("tmp/\n"), 0o644))
	require.NoError(t, analyzer.WriteFile(dir, "tmp/x.txt", "ignored"))

	changes, err := analyzer.DetectGitChanges(dir)
	require.NoError(t, err)
	for _, c := range changes {
		assert.NotEqual(t, "tmp/x.txt", c.Path)
	}
}

func TestDetectGitChanges_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	require.NoError(t, analyzer.WriteFile(dir, "remove.txt", "x"))
	require.NoError(t, exec.Command("git", "-C", dir, "add", "remove.txt").Run())
	require.NoError(t, exec.Command("git", "-C", dir, "commit", "-m", "add").Run())
	require.NoError(t, os.Remove(filepath.Join(dir, "remove.txt")))

	changes, err := analyzer.DetectGitChanges(dir)
	require.NoError(t, err)
	found := false
	for _, c := range changes {
		if c.Path == "remove.txt" && c.Operation == store.OperationDelete {
			found = true
		}
	}
	assert.True(t, found)
}
