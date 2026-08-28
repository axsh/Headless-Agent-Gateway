package llm_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeTier1_Write_ListPaths(t *testing.T) {
	agent := &turnDiffFakeAgent{
		name: "claude-code",
		events: []codingagent.StreamEvent{{
			Type:     codingagent.EventToolUse,
			ToolName: "Write",
			ToolInput: map[string]any{
				"file_path": "hello.txt",
				"content":   "hi",
			},
		}},
	}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, nil)
	postSSEMessage(t, baseURL, sessionID, "create hello")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	AssertSystemArtifactPathsContain(t, page.Items, "hello.txt")
	found := false
	for _, item := range page.Items {
		if filepath.Base(item.Key) == "hello.txt" {
			found = true
			assert.Equal(t, "Write", item.ToolName)
			assert.Equal(t, store.OperationCreate, item.Operation)
		}
	}
	require.True(t, found)
}

func TestClaudeTier1_TwoWrites_ListPaths(t *testing.T) {
	agent := &turnDiffFakeAgent{
		name: "claude-code",
		events: []codingagent.StreamEvent{
			{
				Type:     codingagent.EventToolUse,
				ToolName: "Write",
				ToolInput: map[string]any{"file_path": "a.txt", "content": "A"},
			},
			{
				Type:     codingagent.EventToolUse,
				ToolName: "Write",
				ToolInput: map[string]any{"file_path": "b.txt", "content": "B"},
			},
		},
	}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, nil)
	postSSEMessage(t, baseURL, sessionID, "create a and b")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	AssertSystemArtifactPathsContain(t, page.Items, "a.txt", "b.txt")
}

func TestClaudeTier1_StructuredToolOff(t *testing.T) {
	agent := &turnDiffFakeAgent{
		name: "claude-code",
		events: []codingagent.StreamEvent{{
			Type:     codingagent.EventToolUse,
			ToolName: "Write",
			ToolInput: map[string]any{"file_path": "skip.txt", "content": "x"},
		}},
	}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, map[string]any{
		"structured_tool":   false,
		"shell_parser":      true,
		"workdir_reconcile": false,
	})
	postSSEMessage(t, baseURL, sessionID, "create skip")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	assert.Empty(t, page.Items)
}

func TestClaudeTier1_TurnFilesOnly_ListPaths(t *testing.T) {
	agent := &turnDiffFakeAgent{
		name: "claude-code",
		events: []codingagent.StreamEvent{{
			Type:     codingagent.EventToolUse,
			ToolName: analyzer.ToolNameTurnFiles,
			ToolInput: map[string]any{
				"changes": []any{
					map[string]any{"path": "agg.txt", "kind": "add"},
				},
			},
		}},
	}
	baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, nil)
	postSSEMessage(t, baseURL, sessionID, "aggregate")

	page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs: []string{sessionID},
	})
	require.NoError(t, err)
	AssertSystemArtifactPathsContain(t, page.Items, "agg.txt")
	found := false
	for _, item := range page.Items {
		if filepath.Base(item.Key) == "agg.txt" {
			found = true
			assert.Equal(t, analyzer.ToolNameTurnFiles, item.ToolName)
		}
	}
	require.True(t, found)
}
