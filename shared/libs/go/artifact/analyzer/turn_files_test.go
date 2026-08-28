package analyzer_test

import (
	"context"
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

func TestCollectClaudeTier1OpsFromStream(t *testing.T) {
	events := []codingagent.StreamEvent{
		{Type: codingagent.EventToolUse, ToolName: "Write", ToolInput: map[string]any{"file_path": "a.txt"}},
		{Type: codingagent.EventToolUse, ToolName: "Edit", ToolInput: map[string]any{"file_path": "b.txt"}},
		{Type: codingagent.EventToolUse, ToolName: "Delete", ToolInput: map[string]any{"path": "c.txt"}},
		{Type: codingagent.EventToolUse, ToolName: "Bash", ToolInput: map[string]any{"command": "echo x > d.txt"}},
		{Type: codingagent.EventToolUse, ToolName: "file_change", ToolInput: map[string]any{"path": "e.txt", "kind": "add"}},
		{Type: codingagent.EventResult},
	}
	ops := analyzer.CollectClaudeTier1OpsFromStream(events)
	require.Len(t, ops, 3)
	assert.Equal(t, analyzer.TurnFileOp{Path: "a.txt", Kind: "add"}, ops[0])
	assert.Equal(t, analyzer.TurnFileOp{Path: "b.txt", Kind: "update"}, ops[1])
	assert.Equal(t, analyzer.TurnFileOp{Path: "c.txt", Kind: "delete"}, ops[2])
}

func TestSynthesizeTurnFilesEvent(t *testing.T) {
	assert.Nil(t, analyzer.SynthesizeTurnFilesEvent(nil))

	single := analyzer.SynthesizeTurnFilesEvent([]analyzer.TurnFileOp{{Path: "a.txt", Kind: "add"}})
	require.NotNil(t, single)
	assert.Equal(t, analyzer.ToolNameTurnFiles, single.ToolName)
	assert.Equal(t, "a.txt", single.ToolInput["path"])
	assert.Equal(t, "add", single.ToolInput["kind"])

	multi := analyzer.SynthesizeTurnFilesEvent([]analyzer.TurnFileOp{
		{Path: "a.txt", Kind: "add"},
		{Path: "b.txt", Kind: "update"},
	})
	require.NotNil(t, multi)
	changes, ok := multi.ToolInput["changes"].([]any)
	require.True(t, ok)
	require.Len(t, changes, 2)
}

func TestAnalyzer_ClaudeWrite_FilePath(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{
		"file_path": "hello.txt",
		"content":   "hi",
	})
	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "hello.txt", ms.events[0].Key)
	assert.Equal(t, "Write", ms.events[0].ToolName)
	assert.Equal(t, store.OperationCreate, ms.events[0].Operation)
}

func TestAnalyzer_TurnFiles_SingleAndMulti(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", analyzer.ToolNameTurnFiles, map[string]any{
		"path": "solo.txt",
		"kind": "add",
	})
	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "solo.txt", ms.events[0].Key)
	assert.Equal(t, analyzer.ToolNameTurnFiles, ms.events[0].ToolName)

	ms2 := &memStore{}
	tl2 := tasklog.New()
	analyzer.New(tl2, ms2, workDir, func(string) string { return workDir }, nil)
	injectToolUseEvent(t, tl2, "sess-2", analyzer.ToolNameTurnFiles, map[string]any{
		"changes": []any{
			map[string]any{"path": "a.txt", "kind": "add"},
			map[string]any{"path": "b.txt", "kind": "update"},
		},
	})
	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms2.events, 2)
	keys := map[string]string{}
	for _, e := range ms2.events {
		keys[e.Key] = e.ToolName
	}
	assert.Equal(t, analyzer.ToolNameTurnFiles, keys["a.txt"])
	assert.Equal(t, analyzer.ToolNameTurnFiles, keys["b.txt"])
}

func TestAnalyzer_TurnFiles_StructuredToolOff(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, func(string) codingagent.FileChangeCollectors {
		return codingagent.FileChangeCollectors{StructuredTool: false, ShellParser: true, WorkdirReconcile: false}
	})
	injectToolUseEvent(t, tl, "sess-1", analyzer.ToolNameTurnFiles, map[string]any{
		"path": "x.txt",
		"kind": "add",
	})
	time.Sleep(20 * time.Millisecond)
	require.Empty(t, ms.events)
}

func TestAnalyzer_WriteThenTurnFiles_FirstWins(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	a := analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{
		"file_path": "hello.txt",
	})
	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 1)
	assert.Equal(t, "Write", ms.events[0].ToolName)

	err := a.FlushTurnFiles(context.Background(), "sess-1", "", "", []analyzer.TurnFileOp{
		{Path: "hello.txt", Kind: "add"},
	})
	require.NoError(t, err)
	require.Len(t, ms.events, 1, "turn_files must not add a row when Write already claimed the key")
}

func TestAnalyzer_FlushTurnFiles_WithoutPriorWrite(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	a := analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	err := a.FlushTurnFiles(context.Background(), "sess-1", "turn-1", "corr-1", []analyzer.TurnFileOp{
		{Path: "a.txt", Kind: "add"},
		{Path: "b.txt", Kind: "add"},
	})
	require.NoError(t, err)
	require.Len(t, ms.events, 2)
	for _, e := range ms.events {
		assert.Equal(t, analyzer.ToolNameTurnFiles, e.ToolName)
		assert.Equal(t, "turn-1", e.TurnID)
		assert.Equal(t, "corr-1", e.CorrelationID)
	}
}

func TestAnalyzer_TwoWrites_SameTurn(t *testing.T) {
	ms := &memStore{}
	tl := tasklog.New()
	workDir := filepath.ToSlash(t.TempDir())
	analyzer.New(tl, ms, workDir, func(string) string { return workDir }, nil)

	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{"file_path": "a.txt"})
	injectToolUseEvent(t, tl, "sess-1", "Write", map[string]any{"file_path": "b.txt"})
	time.Sleep(20 * time.Millisecond)
	require.Len(t, ms.events, 2)
	keys := map[string]bool{}
	for _, e := range ms.events {
		keys[e.Key] = true
	}
	assert.True(t, keys["a.txt"])
	assert.True(t, keys["b.txt"])
}
