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
	artifactapi "github.com/axsh/arctic-tern/shared/libs/go/artifact/api"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemArtifact_SeventyUpdatePages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	require.NoError(t, st.UpsertSession(ctx, store.Session{
		ID: "s70", AgentID: "cursor", StartedAt: time.Now(),
	}))
	for i := 0; i < 70; i++ {
		key := fmt.Sprintf("updated/file_%03d.go", i)
		require.NoError(t, st.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s70", AgentID: "cursor", Key: key,
			Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Duration(i) * time.Millisecond),
			ToolName: "Write",
		}))
		require.NoError(t, st.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s70", AgentID: "cursor", Key: key,
			Operation: store.OperationUpdate, OccurredAt: time.Now().Add(time.Hour+time.Duration(i)*time.Millisecond),
			ToolName: "StrReplace",
		}))
	}

	mux := http.NewServeMux()
	artifactapi.NewSystemArtifactHandler(st).RegisterRoutes(mux, "/api/v1/artifacts/system")
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	seen := map[string]struct{}{}
	for pageNum := 1; pageNum <= 3; pageNum++ {
		resp, err := http.Get(fmt.Sprintf(
			"%s/api/v1/artifacts/system?session_id=s70&operation=update&per_page=30&sort=key&order=asc&page=%d",
			ts.URL, pageNum))
		require.NoError(t, err)
		var body struct {
			TotalCount int `json:"total_count"`
			Items      []struct {
				Key string `json:"key"`
			} `json:"items"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		resp.Body.Close()
		assert.Equal(t, 70, body.TotalCount)
		want := 30
		if pageNum == 3 {
			want = 10
		}
		assert.Len(t, body.Items, want)
		for _, it := range body.Items {
			seen[it.Key] = struct{}{}
		}
	}
	assert.Len(t, seen, 70)
}

func TestSystemArtifact_ListAll_Client(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	require.NoError(t, st.UpsertSession(ctx, store.Session{
		ID: "s-all", AgentID: "cursor", StartedAt: time.Now(),
	}))
	for i := 0; i < 120; i++ {
		require.NoError(t, st.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s-all", AgentID: "cursor",
			Key: fmt.Sprintf("listall/file_%03d.go", i),
			Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}))
	}

	mux := http.NewServeMux()
	artifactapi.NewSystemArtifactHandler(st).RegisterRoutes(mux, "/api/v1/artifacts/system")
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c := v1.New(ts.URL)
	all, err := c.SystemArtifacts().ListAll(ctx, v1.SystemArtifactFilter{
		SessionIDs: []string{"s-all"},
	})
	require.NoError(t, err)
	assert.Len(t, all, 120)
}

func TestShellParser_IgnoresDevNull(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "artifacts.db")
	st, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	sessionID := "s-null"
	require.NoError(t, st.UpsertSession(ctx, store.Session{
		ID: sessionID, AgentID: "cursor", StartedAt: time.Now(),
	}))

	tl := tasklog.New()
	_ = analyzer.New(tl, st, "/proj", func(string) string { return "/proj" })

	ev := codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "Bash",
		ToolInput: map[string]any{
			"command": "echo hi > /dev/null && echo x > out.txt",
		},
	}
	body, err := json.Marshal(ev)
	require.NoError(t, err)
	tl.Add(tasklog.NewAgentLogSendEntry("log1", sessionID, string(body)))
	time.Sleep(50 * time.Millisecond)

	page, err := st.ListAllSystemArtifacts(ctx, store.SystemArtifactFilter{SessionIDs: []string{sessionID}})
	require.NoError(t, err)
	keys := map[string]struct{}{}
	for _, it := range page {
		keys[it.Key] = struct{}{}
	}
	_, hasNull := keys["null"]
	_, hasDevNull := keys["/dev/null"]
	assert.False(t, hasNull)
	assert.False(t, hasDevNull)
	_, hasOut := keys["out.txt"]
	assert.True(t, hasOut)
}
