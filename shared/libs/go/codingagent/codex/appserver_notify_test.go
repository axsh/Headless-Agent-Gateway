package codex

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestParseAppServerNotification_TurnDiffUpdated(t *testing.T) {
	line := `{"method":"turn/diff/updated","params":{"threadId":"t1","turnId":"u1","diff":"--- /dev/null\n+++ b/hello.txt\n@@ -0,0 +1 @@\n+hi\n"}}`
	ev := ParseAppServerNotification(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.ToolName != ToolNameTurnDiff || ev.Type != codingagent.EventToolUse {
		t.Fatalf("got %s/%s", ev.Type, ev.ToolName)
	}
	if ev.ToolInput["path"] != "hello.txt" {
		t.Fatalf("path=%v", ev.ToolInput["path"])
	}
}

func TestParseAppServerNotification_UnknownAndBad(t *testing.T) {
	if ParseAppServerNotification(`{"method":"turn/started","params":{}}`) != nil {
		t.Fatal("unknown method should be nil")
	}
	if ParseAppServerNotification(`not-json`) != nil {
		t.Fatal("bad json should be nil")
	}
}
