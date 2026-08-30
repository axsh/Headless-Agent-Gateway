package llm_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupShellGuardStore(t *testing.T) (store.ArtifactStore, *tasklog.TaskLog, string, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	workDir := t.TempDir()
	sessionID := "s-shell-guard"
	require.NoError(t, st.UpsertSession(context.Background(), store.Session{
		ID: sessionID, AgentID: "codex", StartedAt: time.Now(),
	}))

	tl := tasklog.New()
	_ = analyzer.New(tl, st, workDir, func(string) string { return workDir }, nil)
	return st, tl, workDir, sessionID
}

func injectShellGuardEvent(t *testing.T, tl *tasklog.TaskLog, sessionID string, ev codingagent.StreamEvent) {
	t.Helper()
	body, err := json.Marshal(ev)
	require.NoError(t, err)
	tl.Add(tasklog.NewAgentLogSendEntry("log-shell-guard", sessionID, string(body)))
}

func TestE2E_ShellParser_ExistenceGuard_KeepsExistingCreate(t *testing.T) {
	st, tl, workDir, sessionID := setupShellGuardStore(t)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "actual.txt"), []byte("hi"), 0o644))

	injectShellGuardEvent(t, tl, sessionID, codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "command_execution",
		ToolInput: map[string]any{
			"command":          "echo hi > actual.txt",
			"execution_status": "completed",
		},
	})
	time.Sleep(50 * time.Millisecond)

	page, err := st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "actual.txt", page[0].Key)
	assert.Equal(t, store.OperationCreate, page[0].Operation)
}

func TestE2E_ShellParser_ExistenceGuard_DropsMissingCreate(t *testing.T) {
	st, tl, _, sessionID := setupShellGuardStore(t)

	missing := filepath.ToSlash(filepath.Join(t.TempDir(), "definitely_not_exist_xyz_12345.txt"))
	injectShellGuardEvent(t, tl, sessionID, codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "command_execution",
		ToolInput: map[string]any{
			"command":          "echo hi > " + missing,
			"execution_status": "completed",
		},
	})
	time.Sleep(50 * time.Millisecond)

	page, err := st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	assert.Empty(t, page)
}

func TestE2E_ShellParser_ExistenceGuard_BashDefer(t *testing.T) {
	st, tl, workDir, sessionID := setupShellGuardStore(t)

	injectShellGuardEvent(t, tl, sessionID, codingagent.StreamEvent{
		Type:       codingagent.EventToolUse,
		ToolName:   "Bash",
		ToolCallID: "tu_e2e",
		ToolInput:  map[string]any{"command": "echo hi > new.txt"},
	})
	time.Sleep(30 * time.Millisecond)
	page, err := st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	assert.Empty(t, page)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "new.txt"), []byte("hi"), 0o644))
	injectShellGuardEvent(t, tl, sessionID, codingagent.StreamEvent{
		Type:       codingagent.EventToolResult,
		ToolCallID: "tu_e2e",
		Content:    "ok",
	})
	time.Sleep(50 * time.Millisecond)

	page, err = st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "new.txt", page[0].Key)
}

func TestE2E_ShellParser_ExistenceGuard_DeleteWithoutFile(t *testing.T) {
	st, tl, _, sessionID := setupShellGuardStore(t)

	injectShellGuardEvent(t, tl, sessionID, codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "command_execution",
		ToolInput: map[string]any{
			"command":          "rm gone.txt",
			"execution_status": "completed",
		},
	})
	time.Sleep(50 * time.Millisecond)

	page, err := st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs:     []string{sessionID},
		IncludeDeleted: true,
	})
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, "gone.txt", page[0].Key)
	assert.Equal(t, store.OperationDelete, page[0].Operation)
}
