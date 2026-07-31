package tasklog

import (
	"testing"
	"time"
)

func TestTaskLog_AddAndClone(t *testing.T) {
	log := New()
	if len(log.Entries()) != 0 {
		t.Errorf("expected empty log, got %d entries", len(log.Entries()))
	}

	entry1 := &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        "1",
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID: "agent-1",
		Body:    "message 1",
	}

	log.Add(entry1)
	if len(log.Entries()) != 1 {
		t.Errorf("expected 1 entry, got %d", len(log.Entries()))
	}

	clone := log.Clone()
	if len(clone.Entries()) != 1 {
		t.Errorf("expected clone to have 1 entry, got %d", len(clone.Entries()))
	}
}

func TestTaskLog_SetOnEntryChainsHandlers(t *testing.T) {
	log := New()
	var calls []string
	log.SetOnEntry(func(Entry) { calls = append(calls, "first") })
	log.SetOnEntry(func(Entry) { calls = append(calls, "second") })

	log.Add(&AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        "1",
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID: "agent-1",
	})

	if len(calls) != 2 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("expected chained handler calls [first second], got %v", calls)
	}
}

func TestTaskLog_AbnormalTerminationAutoClose(t *testing.T) {
	log := New()

	entry1 := &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        "log-1",
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID:    "agent-1",
		IsComplete: false,
		Phase:      "begin",
	}
	entry2 := &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        "log-2",
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID:    "agent-1",
		IsComplete: true,
		Phase:      "end",
	}

	log.Add(entry1)
	log.Add(entry2)

	term := NewTerminatedEntry("agent-1", "Abnormal crash")
	log.Add(term)

	entries := log.Entries()
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// entry1 (index 0) should be automatically closed
	e1, ok := entries[0].(*AgentLogEntry)
	if !ok {
		t.Fatalf("expected AgentLogEntry at index 0")
	}
	if !e1.IsComplete {
		t.Errorf("expected entry1 to be complete after termination")
	}
	expectedSuffix := "[auto-closed: abnormal termination]"
	if len(e1.Body) < len(expectedSuffix) || e1.Body[len(e1.Body)-len(expectedSuffix):] != expectedSuffix {
		t.Errorf("expected entry1 body to end with %q, got %q", expectedSuffix, e1.Body)
	}

	// entry2 (index 1) should remain complete and untouched (it was already complete)
	e2, ok := entries[1].(*AgentLogEntry)
	if !ok {
		t.Fatalf("expected AgentLogEntry at index 1")
	}
	if !e2.IsComplete {
		t.Errorf("expected entry2 to remain complete")
	}
	if e2.Body != "" {
		t.Errorf("expected entry2 body to remain empty, got %q", e2.Body)
	}
}
