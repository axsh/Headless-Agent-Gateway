package agentservice

import (
	"context"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

func TestWrapPromptForSelfHeal(t *testing.T) {
	dir := t.TempDir()
	c := session.OpenCanonical(dir)
	if err := c.Init("sess", "codex"); err != nil {
		t.Fatal(err)
	}
	if err := c.Append([]session.Message{
		{Role: "user", Content: "prior user", Origin: "codex"},
		{Role: "assistant", Content: "prior assistant fact", Origin: "codex"},
		{Role: "user", Content: "CURRENT-USER-TURN", Origin: "codex"},
	}); err != nil {
		t.Fatal(err)
	}
	s := New()
	rec := &codingagent.SessionRecord{ID: "sess", AgentName: "codex", SessionDir: dir, Model: "gpt-4o"}
	got, err := s.wrapPromptForSelfHeal(context.Background(), rec, "latest question")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, portable.TransferHeader) {
		t.Fatalf("missing transfer header: %s", got)
	}
	if !strings.Contains(got, "prior assistant fact") {
		t.Fatalf("missing prior assistant: %s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "latest question") {
		t.Fatalf("user prompt should be last: %s", got)
	}
	if strings.Contains(got, "CURRENT-USER-TURN") {
		t.Fatalf("current user turn should not be duplicated in supplement: %s", got)
	}
}

func TestWrapPromptForSelfHeal_EmptySessionDir(t *testing.T) {
	s := New()
	rec := &codingagent.SessionRecord{AgentName: "codex"}
	got, err := s.wrapPromptForSelfHeal(context.Background(), rec, "plain")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain" {
		t.Fatalf("got %q, want plain", got)
	}
}
