package agentservice

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileSessionArtifacts_FlushTurnFiles(t *testing.T) {
	workDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	tl := tasklog.New()
	srv := New(WithTaskLog(tl), WithArtifactStore(st, workDir))
	require.NotNil(t, srv.toolAnalyzer)

	sessionID := "flush-sess-1"
	require.NoError(t, srv.sessions.Create(&codingagent.SessionRecord{
		ID:      sessionID,
		WorkDir: workDir,
		Status:  codingagent.StatusActive,
		FileChangeCollectors: &codingagent.FileChangeCollectors{
			StructuredTool:   true,
			ShellParser:      true,
			WorkdirReconcile: false, // Tier3 off; Flush must still run
		},
	}))

	src := make(chan codingagent.StreamEvent, 4)
	src <- codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "Write",
		ToolInput: map[string]any{
			"file_path": "a.txt",
			"content":   "A",
		},
	}
	src <- codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "Write",
		ToolInput: map[string]any{
			"file_path": "b.txt",
			"content":   "B",
		},
	}
	close(src)
	relay := newEventRelay(src)
	deadline := time.Now().Add(2 * time.Second)
	for !relay.isSourceDone() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, relay.isSourceDone())

	exec := &activeExecution{
		sessionID:     sessionID,
		turnID:        "turn-flush",
		correlationID: "corr-flush",
		relay:         relay,
		status:        codingagent.StatusActive,
	}
	require.NoError(t, srv.execRegistry.Register(sessionID, exec))

	srv.reconcileSessionArtifacts(context.Background(), sessionID, "turn-flush", "corr-flush")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, page.TotalCount, 2)
	found := map[string]string{}
	for _, item := range page.Items {
		found[filepath.Base(item.Key)] = item.ToolName
	}
	assert.Equal(t, "turn_files", found["a.txt"])
	assert.Equal(t, "turn_files", found["b.txt"])
}
