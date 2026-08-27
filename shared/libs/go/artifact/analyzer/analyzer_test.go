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

func (m *memStore) UpsertSession(_ context.Context, _ store.Session) error { return nil }
func (m *memStore) CloseSession(_ context.Context, _ string) error         { return nil }
func (m *memStore) SaveSystemArtifactEvent(_ context.Context, e store.SystemArtifactEvent) error {
	m.events = append(m.events, e)
	return nil
}
func (m *memStore) ListSystemArtifacts(_ context.Context, _ store.SystemArtifactFilter) (*store.SystemArtifactPage, error) {
	return &store.SystemArtifactPage{}, nil
}
func (m *memStore) ListAllSystemArtifacts(_ context.Context, _ store.SystemArtifactFilter) ([]store.SystemArtifactEvent, error) {
	return append([]store.SystemArtifactEvent(nil), m.events...), nil
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
func (m *memStore) ListAllUserArtifacts(_ context.Context, _ store.UserArtifactFilter) ([]store.UserArtifact, error) {
	return nil, nil
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

	analyzer.New(tl, ms, projectRoot, nil, nil)

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

	analyzer.New(tl, ms, projectRoot, nil, nil)

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

	analyzer.New(tl, ms, projectRoot, nil, nil)

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

	analyzer.New(tl, ms, projectRoot, nil, nil)

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

	analyzer.New(tl, ms, projectRoot, nil, nil)

	// A plain text event should not produce any artifact events.
	tl.Add(tasklog.NewAgentLogSendEntry("log-1", "sess-1", `{"type":"text","content":"hello"}`))

	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, ms.events)
}

func TestAnalyzer_UnknownTool_Ignored(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())

	analyzer.New(tl, ms, projectRoot, nil, nil)

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

	analyzer.New(tl, ms, projectRoot, nil, nil)

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

func TestAnalyzer_Codex_FileChange_Create(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(sessionID string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "file_change", map[string]any{
		"path": "docs/a.md",
		"kind": "add",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "docs/a.md", ms.events[0].Key)
	assert.Equal(t, store.OperationCreate, ms.events[0].Operation)
	assert.Equal(t, "file_change", ms.events[0].ToolName)
}

func TestAnalyzer_Codex_FileChange_Multiple(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, nil, nil)

	injectToolUseEvent(t, tl, "sess-1", "file_change", map[string]any{
		"changes": []any{
			map[string]any{"path": "a.md", "kind": "add"},
			map[string]any{"path": "b.md", "kind": "update"},
		},
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 2)
}

func TestAnalyzer_Codex_FileChange_Delete(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	analyzer.New(tl, ms, t.TempDir(), nil, nil)

	injectToolUseEvent(t, tl, "sess-1", "file_change", map[string]any{
		"path": "gone.txt",
		"kind": "delete",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, store.OperationDelete, ms.events[0].Operation)
}

func TestAnalyzer_Codex_CommandExecution_Create(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "command_execution", map[string]any{
		"command": "echo hi > out.txt",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "out.txt", ms.events[0].Key)
	assert.Equal(t, store.OperationCreate, ms.events[0].Operation)
}

func TestAnalyzer_ClaudeCode_Bash_Create(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "Bash", map[string]any{
		"command": "echo hi > out.txt",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "out.txt", ms.events[0].Key)
	assert.Equal(t, "Bash", ms.events[0].ToolName)
}

func TestAnalyzer_ClaudeCode_NotebookEdit(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, nil, nil)

	injectToolUseEvent(t, tl, "sess-1", "NotebookEdit", map[string]any{
		"notebook_path": "nb.ipynb",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "nb.ipynb", ms.events[0].Key)
	assert.Equal(t, store.OperationUpdate, ms.events[0].Operation)
}

func TestAnalyzer_CommandExecution_NoFileOp(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	analyzer.New(tl, ms, t.TempDir(), nil, nil)

	injectToolUseEvent(t, tl, "sess-1", "command_execution", map[string]any{
		"command": "ls -la",
	})

	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, ms.events)
}

func TestAnalyzer_WorkDirRelativePath(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, "/other-root", func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{
		"path": "subdir/file.go",
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "subdir/file.go", ms.events[0].Key)
}

func TestAnalyzer_LegacyShell_Create(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "shell", map[string]any{
		"arguments": `{"command":"echo hi > legacy.txt"}`,
	})

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "legacy.txt", ms.events[0].Key)
}

func TestAnalyzer_PropagatesTurnContext(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, projectRoot, nil, nil)

	body := `{"type":"tool_use","tool_name":"Write","turn_id":"turn-1","correlation_id":"corr-1","tool_input":{"path":"main.go"}}`
	tl.Add(tasklog.NewAgentLogSendEntry("log-turn", "sess-1", body))

	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "turn-1", ms.events[0].TurnID)
	assert.Equal(t, "corr-1", ms.events[0].CorrelationID)
}

func TestAnalyzer_StructuredToolOff(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	projectRoot := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, projectRoot, nil, func(string) codingagent.FileChangeCollectors {
		return codingagent.FileChangeCollectors{StructuredTool: false, ShellParser: true, WorkdirReconcile: false}
	})

	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{
		"path": projectRoot + "/a.go",
	})
	time.Sleep(20 * time.Millisecond)
	require.Empty(t, ms.events)
}

func TestAnalyzer_ShellParserOff(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, func(string) codingagent.FileChangeCollectors {
		return codingagent.FileChangeCollectors{StructuredTool: true, ShellParser: false, WorkdirReconcile: false}
	})

	injectToolUseEvent(t, tl, "sess-1", "Bash", map[string]any{
		"command": "echo hi > out.txt",
	})
	time.Sleep(20 * time.Millisecond)
	require.Empty(t, ms.events)
}
