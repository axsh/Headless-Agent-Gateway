package analyzer_test

import (
	"context"
	"encoding/json"
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

// memStore is an in-memory ArtifactStore for testing.
type memStore struct {
	events []store.SystemArtifactEvent
}

func (m *memStore) UpsertSession(_ context.Context, _ store.Session) error        { return nil }
func (m *memStore) CloseSession(_ context.Context, _ string) error                { return nil }
func (m *memStore) SaveSystemArtifactEvent(_ context.Context, e store.SystemArtifactEvent) error {
	m.events = append(m.events, e)
	return nil
}
func (m *memStore) ListSystemArtifacts(_ context.Context, _ store.SystemArtifactFilter) (*store.SystemArtifactPage, error) {
	return &store.SystemArtifactPage{}, nil
}
func (m *memStore) GetSystemArtifactByKey(_ context.Context, _ string) ([]store.SystemArtifactEvent, error) {
	return nil, nil
}
func (m *memStore) SaveUserArtifact(_ context.Context, _ store.UserArtifact) error { return nil }
func (m *memStore) GetUserArtifactByKey(_ context.Context, _ string) (*store.UserArtifact, error) {
	return nil, nil
}
func (m *memStore) ListUserArtifacts(_ context.Context, _ store.UserArtifactFilter) (*store.UserArtifactPage, error) {
	return &store.UserArtifactPage{}, nil
}
func (m *memStore) DeleteUserArtifact(_ context.Context, _ string) error { return nil }
func (m *memStore) Close() error                                         { return nil }

// injectToolUseEvent simulates receiving a tool_use StreamEvent via the TaskLog.
// It replicates the same JSON serialization that agentservice uses in toAgentLogEntry.
func injectToolUseEvent(t *testing.T, tl *tasklog.TaskLog, sessionID, toolName string, input map[string]any) {
	t.Helper()
	ev := codingagent.StreamEvent{
		Type:      codingagent.EventToolUse,
		ToolName:  toolName,
		ToolInput: input,
	}
	body, err := json.Marshal(ev)
	require.NoError(t, err)
	entry := tasklog.NewAgentLogSendEntry("log-test", sessionID, string(body))
	tl.Add(entry)
}

func TestAnalyzer_CursorWrite(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil)

	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{
		"path":     projectRoot + "/internal/user.go",
		"contents": "package user",
	})

	// Wait for async handler.
	time.Sleep(20 * time.Millisecond)

	require.Len(t, ms.events, 1)
	assert.Equal(t, "internal/user.go", ms.events[0].Key)
	assert.Equal(t, store.OperationCreate, ms.events[0].Operation)
	assert.Equal(t, "Write", ms.events[0].ToolName)
}

func TestAnalyzer_CursorStrReplace(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil)

	injectToolUseEvent(t, tl, "sess-1", "StrReplace", map[string]any{
		"path": projectRoot + "/a.go",
	})

	time.Sleep(20 * time.Millisecond)

	require.Len(t, ms.events, 1)
	assert.Equal(t, "a.go", ms.events[0].Key)
	assert.Equal(t, store.OperationUpdate, ms.events[0].Operation)
}

func TestAnalyzer_CursorDelete(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil)

	injectToolUseEvent(t, tl, "sess-1", "Delete", map[string]any{
		"path": projectRoot + "/tmp/old.go",
	})

	time.Sleep(20 * time.Millisecond)

	require.Len(t, ms.events, 1)
	assert.Equal(t, "tmp/old.go", ms.events[0].Key)
	assert.Equal(t, store.OperationDelete, ms.events[0].Operation)
}

func TestAnalyzer_ClaudeCodeEdit(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil)

	// Claude Code uses "file_path" instead of "path".
	injectToolUseEvent(t, tl, "sess-1", "Edit", map[string]any{
		"file_path": projectRoot + "/b.go",
	})

	time.Sleep(20 * time.Millisecond)

	require.Len(t, ms.events, 1)
	assert.Equal(t, "b.go", ms.events[0].Key)
	assert.Equal(t, store.OperationUpdate, ms.events[0].Operation)
}

func TestAnalyzer_TextEvent_Ignored(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil)

	// A plain text event should not produce any artifact events.
	tl.Add(tasklog.NewAgentLogSendEntry("log-1", "sess-1", `{"type":"text","content":"hello"}`))

	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, ms.events)
}

func TestAnalyzer_UnknownTool_Ignored(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil)

	injectToolUseEvent(t, tl, "sess-1", "Read", map[string]any{
		"path": projectRoot + "/a.go",
	})

	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, ms.events)
}

func TestAnalyzer_PathOutsideProjectRoot(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := "/myproject"

	analyzer.New(tl, ms, projectRoot, nil)

	// Path outside project root should be stored as-is (no panic).
	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{
		"path":     "/other/file.go",
		"contents": "x",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	// Key should be the path as-is or a relative escape.
	assert.NotEmpty(t, ms.events[0].Key)
}
