package agentservice_test

import (
	"testing"

	"github.com/axsh/hag/agentservice"
	"github.com/axsh/hag/codingagent"
)

func TestMemorySessionStore_Create(t *testing.T) {
	store := agentservice.NewMemorySessionStore()
	record := &codingagent.SessionRecord{
		ID:        "sess-1",
		AgentName: "claudecode",
		Status:    codingagent.StatusActive,
	}

	if err := store.Create(record); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	got, err := store.Get("sess-1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got.AgentName != "claudecode" {
		t.Errorf("AgentName = %v, want claudecode", got.AgentName)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestMemorySessionStore_Update(t *testing.T) {
	store := agentservice.NewMemorySessionStore()
	record := &codingagent.SessionRecord{
		ID:     "sess-1",
		Status: codingagent.StatusActive,
	}
	store.Create(record)

	record.Status = codingagent.StatusCompleted
	record.SDKSessionID = "sdk-abc"
	if err := store.Update(record); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	got, _ := store.Get("sess-1")
	if got.Status != codingagent.StatusCompleted {
		t.Errorf("Status = %v, want completed", got.Status)
	}
	if got.SDKSessionID != "sdk-abc" {
		t.Errorf("SDKSessionID = %v, want sdk-abc", got.SDKSessionID)
	}
}

func TestMemorySessionStore_List(t *testing.T) {
	store := agentservice.NewMemorySessionStore()
	for _, id := range []string{"a", "b", "c"} {
		store.Create(&codingagent.SessionRecord{ID: id, Status: codingagent.StatusActive})
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("len(list) = %d, want 3", len(list))
	}
}

func TestMemorySessionStore_Delete(t *testing.T) {
	store := agentservice.NewMemorySessionStore()
	store.Create(&codingagent.SessionRecord{ID: "sess-1", Status: codingagent.StatusActive})

	if err := store.Delete("sess-1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	_, err := store.Get("sess-1")
	if err != agentservice.ErrNotFound {
		t.Errorf("Get after delete: error = %v, want ErrNotFound", err)
	}
}

func TestMemorySessionStore_GetNotFound(t *testing.T) {
	store := agentservice.NewMemorySessionStore()
	_, err := store.Get("nonexistent")
	if err != agentservice.ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestMemorySessionStore_StatusTransition(t *testing.T) {
	tests := []struct {
		name  string
		from  string
		to    string
		valid bool
	}{
		{"active to completed", codingagent.StatusActive, codingagent.StatusCompleted, true},
		{"active to error", codingagent.StatusActive, codingagent.StatusError, true},
		{"active to closed", codingagent.StatusActive, codingagent.StatusClosed, true},
		{"completed to active (invalid)", codingagent.StatusCompleted, codingagent.StatusActive, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := agentservice.NewMemorySessionStore()
			record := &codingagent.SessionRecord{
				ID:     "sess-transition",
				Status: tt.from,
			}
			store.Create(record)

			record.Status = tt.to
			err := store.Update(record)

			if tt.valid {
				if err != nil {
					t.Errorf("expected valid transition %s -> %s, got error: %v", tt.from, tt.to, err)
				}
				got, _ := store.Get("sess-transition")
				if got.Status != tt.to {
					t.Errorf("Status = %v, want %v", got.Status, tt.to)
				}
			} else {
				if err == nil {
					t.Errorf("expected error for invalid transition %s -> %s, got nil", tt.from, tt.to)
				}
			}
		})
	}
}

