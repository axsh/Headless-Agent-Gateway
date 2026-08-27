package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	artifactapi "github.com/axsh/arctic-tern/shared/libs/go/artifact/api"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_SystemArtifact_TurnScopedList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	require.NoError(t, st.UpsertSession(ctx, store.Session{
		ID: "sess-turn", AgentID: "cursor", StartedAt: time.Now(),
	}))
	require.NoError(t, st.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "sess-turn", AgentID: "cursor", TurnID: "turn-1",
		Key: "a.txt", Operation: store.OperationCreate, OccurredAt: time.Now(),
	}))
	require.NoError(t, st.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "sess-turn", AgentID: "cursor", TurnID: "turn-2",
		Key: "b.txt", Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Second),
	}))

	mux := http.NewServeMux()
	artifactapi.NewSystemArtifactHandler(st).RegisterRoutes(mux, "/api/v1/artifacts/system")
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c := v1.New(ts.URL)
	page, err := c.SystemArtifacts().List(ctx, v1.SystemArtifactFilter{
		SessionIDs: []string{"sess-turn"},
		TurnIDs:    []string{"turn-2"},
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "b.txt", page.Items[0].Key)
	assert.Equal(t, "turn-2", page.Items[0].TurnID)

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/artifacts/system?turn_id=turn-1", ts.URL))
	require.NoError(t, err)
	defer resp.Body.Close()
	var body struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Key    string `json:"key"`
			TurnID string `json:"turn_id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 1, body.TotalCount)
	require.Len(t, body.Items, 1)
	assert.Equal(t, "a.txt", body.Items[0].Key)
	assert.Equal(t, "turn-1", body.Items[0].TurnID)
}

func TestE2E_SystemArtifact_AnalyzerPropagatesTurnAndCorrelation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	sessionID := "sess-analyzer-turn"
	require.NoError(t, st.UpsertSession(ctx, store.Session{
		ID: sessionID, AgentID: "cursor", StartedAt: time.Now(),
	}))

	tl := tasklog.New()
	_ = analyzer.New(tl, st, "/proj", func(string) string { return "/proj" }, nil)

	ev := codingagent.StreamEvent{
		Type:          codingagent.EventToolUse,
		ToolName:      "Write",
		TurnID:        "turn-xyz",
		CorrelationID: "corr-xyz",
		ToolInput: map[string]any{
			"path": "out.txt",
		},
	}
	raw, err := json.Marshal(ev)
	require.NoError(t, err)
	tl.Add(tasklog.NewAgentLogSendEntry("log-turn-1", sessionID, string(raw)))
	time.Sleep(50 * time.Millisecond)

	all, err := st.ListAllSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
		TurnIDs:    []string{"turn-xyz"},
	})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "out.txt", all[0].Key)
	assert.Equal(t, "turn-xyz", all[0].TurnID)
	assert.Equal(t, "corr-xyz", all[0].CorrelationID)
}
