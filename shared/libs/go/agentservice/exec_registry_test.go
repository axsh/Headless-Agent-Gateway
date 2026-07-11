package agentservice

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestExecRegistry_RegisterRejectsDuplicate(t *testing.T) {
	reg := newExecRegistry()
	exec := &activeExecution{sessionID: "s1", status: codingagent.StatusActive}
	if err := reg.Register("s1", exec); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := reg.Register("s1", exec); err == nil {
		t.Fatal("expected ErrSessionBusy")
	}
}

func TestExecRegistry_SetStatus(t *testing.T) {
	reg := newExecRegistry()
	exec := &activeExecution{sessionID: "s1", status: codingagent.StatusActive}
	_ = reg.Register("s1", exec)
	reg.SetStatus("s1", codingagent.StatusSuspended)
	got, ok := reg.Get("s1")
	if !ok || got.status != codingagent.StatusSuspended {
		t.Fatalf("status = %q, ok=%v", got.status, ok)
	}
}
