package llm_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

func TestIntegration_HierarchicalLogStreaming(t *testing.T) {
	log := tasklog.New()
	stack := &tasklog.LogStack{}

	// Root log
	root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	log.Add(root)
	stack.Push(root.ID)

	if stack.CurrentParentID() != root.ID {
		t.Errorf("expected parent log ID %q, got %q", root.ID, stack.CurrentParentID())
	}

	// Nested thinking log
	thinking := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("thinking"), tasklog.WithParentLogID(stack.CurrentParentID()))
	log.Add(thinking)
	stack.Push(thinking.ID)

	if thinking.ParentLogID != root.ID {
		t.Errorf("expected parent link to be %q, got %q", root.ID, thinking.ParentLogID)
	}

	// Send chunk
	send := tasklog.NewAgentLogSendEntry(thinking.ID, "agent-1", "thought chunk")
	log.Add(send)

	// End thinking log
	endThinking := tasklog.NewAgentLogEndEntry(thinking.ID, "agent-1")
	log.Add(endThinking)
	stack.Pop()

	if stack.CurrentParentID() != root.ID {
		t.Errorf("expected parent back to %q, got %q", root.ID, stack.CurrentParentID())
	}

	// End root
	endRoot := tasklog.NewAgentLogEndEntry(root.ID, "agent-1")
	log.Add(endRoot)
	stack.Pop()

	if stack.CurrentParentID() != "" {
		t.Errorf("expected empty stack, got %q", stack.CurrentParentID())
	}
}

func TestIntegration_AbnormalTerminationAutoClose(t *testing.T) {
	log := tasklog.New()

	// Add incomplete entries
	e1 := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	log.Add(e1)

	// Add terminated entry
	term := tasklog.NewTerminatedEntry("agent-1", "unexpected crash")
	log.Add(term)

	entries := log.Entries()
	e1Updated := entries[0].(*tasklog.AgentLogEntry)
	if !e1Updated.IsComplete {
		t.Errorf("expected e1 to be auto-closed")
	}
	expectedSuffix := "[auto-closed: abnormal termination]"
	if len(e1Updated.Body) < len(expectedSuffix) || e1Updated.Body[len(e1Updated.Body)-len(expectedSuffix):] != expectedSuffix {
		t.Errorf("expected e1 body to contain auto-closed suffix, got %q", e1Updated.Body)
	}
}
