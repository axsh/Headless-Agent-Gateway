package codex

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestExtractPathsFromUnifiedDiff(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []DiffPathOp
	}{
		{
			name: "add",
			diff: "--- /dev/null\n+++ b/hello.txt\n@@ -0,0 +1 @@\n+hi\n",
			want: []DiffPathOp{{Path: "hello.txt", Kind: "add"}},
		},
		{
			name: "update",
			diff: "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n",
			want: []DiffPathOp{{Path: "a.go", Kind: "update"}},
		},
		{
			name: "delete",
			diff: "--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n",
			want: []DiffPathOp{{Path: "gone.txt", Kind: "delete"}},
		},
		{
			name: "multiple",
			diff: "--- /dev/null\n+++ b/one.txt\n@@ -0,0 +1 @@\n+a\n--- a/two.txt\n+++ b/two.txt\n@@ -1 +1 @@\n-b\n+c\n",
			want: []DiffPathOp{
				{Path: "one.txt", Kind: "add"},
				{Path: "two.txt", Kind: "update"},
			},
		},
		{
			name: "empty",
			diff: "   ",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPathsFromUnifiedDiff(tt.diff)
			if len(got) != len(tt.want) {
				t.Fatalf("len=%d want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d]=%+v want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseTurnDiffUpdatedParams_Multiple(t *testing.T) {
	params := []byte(`{
		"threadId":"th-1",
		"turnId":"turn-9",
		"diff":"--- /dev/null\n+++ b/hello.txt\n@@ -0,0 +1 @@\n+x\n--- a/b.go\n+++ b/b.go\n@@ -1 +1 @@\n-y\n+z\n"
	}`)
	ev := ParseTurnDiffUpdatedParams(params)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != codingagent.EventToolUse || ev.ToolName != ToolNameTurnDiff {
		t.Fatalf("type/name = %s/%s", ev.Type, ev.ToolName)
	}
	if ev.TurnID != "turn-9" {
		t.Errorf("TurnID=%q", ev.TurnID)
	}
	changes, ok := ev.ToolInput["changes"].([]any)
	if !ok || len(changes) != 2 {
		t.Fatalf("changes=%v", ev.ToolInput["changes"])
	}
}

func TestTurnDiffStreamEvent_Single(t *testing.T) {
	ev := TurnDiffStreamEvent([]DiffPathOp{{Path: "hello.txt", Kind: "add"}})
	if ev == nil || ev.ToolInput["path"] != "hello.txt" || ev.ToolInput["kind"] != "add" {
		t.Fatalf("%+v", ev)
	}
}
