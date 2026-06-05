package codingagent

import "time"

// SessionStore is the abstract interface for session persistence.
type SessionStore interface {
	Create(session *SessionRecord) error
	Get(id string) (*SessionRecord, error)
	Update(session *SessionRecord) error
	List() ([]*SessionRecord, error)
	Delete(id string) error
}

// SessionRecord is a persisted session record.
type SessionRecord struct {
	ID           string    // HAG-managed session ID (UUID)
	AgentName    string    // "claudecode", "codex"
	Model        string
	Status       string    // "active", "completed", "error", "closed"
	WorkDir      string
	SDKSessionID string    // CLI/SDK-managed session ID (for context resume)
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session status constants.
const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusError     = "error"
	StatusClosed    = "closed"
)
