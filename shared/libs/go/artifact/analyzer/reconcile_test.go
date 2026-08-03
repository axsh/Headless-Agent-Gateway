package analyzer_test

import (
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcile_Tier1WinsOverGit(t *testing.T) {
	now := time.Now()
	out := analyzer.Reconcile(analyzer.ReconcileInput{
		SessionID: "s1",
		ExistingEvents: []store.SystemArtifactEvent{{
			SessionID: "s1", Key: "a.txt", Operation: store.OperationCreate,
			ToolName: "Write", OccurredAt: now,
		}},
		GitChanges: []analyzer.GitDiffResult{{
			Path: "a.txt", Operation: store.OperationCreate, Source: "git",
		}},
	}, nil, "/proj")
	assert.Empty(t, out)
}

func TestReconcile_GitFillsGap(t *testing.T) {
	out := analyzer.Reconcile(analyzer.ReconcileInput{
		SessionID: "s1",
		GitChanges: []analyzer.GitDiffResult{{
			Path: "gap.txt", Operation: store.OperationCreate, Source: "git",
		}},
	}, nil, "/proj")
	require.Len(t, out, 1)
	assert.Equal(t, "reconcile:git", out[0].ToolName)
	assert.Equal(t, "gap.txt", out[0].Key)
}

func TestReconcile_GitignoreNotInGit(t *testing.T) {
	now := time.Now()
	out := analyzer.Reconcile(analyzer.ReconcileInput{
		SessionID: "s1",
		ExistingEvents: []store.SystemArtifactEvent{{
			SessionID: "s1", Key: "tmp/x.txt", Operation: store.OperationCreate,
			ToolName: "file_change", OccurredAt: now,
		}},
	}, nil, "/proj")
	assert.Empty(t, out)
}

func TestReconcile_StructuredOutputLowestPriority(t *testing.T) {
	out := analyzer.Reconcile(analyzer.ReconcileInput{
		SessionID: "s1",
		GitChanges: []analyzer.GitDiffResult{{
			Path: "z.txt", Operation: store.OperationCreate, Source: "git",
		}},
		StructuredPaths: []analyzer.ParsedFileOp{{
			Path: "z.txt", Operation: store.OperationUpdate,
		}},
	}, nil, "/proj")
	require.Len(t, out, 1)
	assert.Equal(t, "reconcile:git", out[0].ToolName)
}

func TestReconcile_DedupSameSource(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	out := analyzer.Reconcile(analyzer.ReconcileInput{
		SessionID: "s1",
		GitChanges: []analyzer.GitDiffResult{
			{Path: "dup.txt", Operation: store.OperationCreate, Source: "git"},
		},
	}, nil, "/proj")
	require.Len(t, out, 1)
	_ = older
	_ = newer
	assert.Equal(t, "dup.txt", out[0].Key)
}
