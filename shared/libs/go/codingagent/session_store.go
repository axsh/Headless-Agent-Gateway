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
	ID           string    `json:"id"`
	AgentName    string    `json:"agent_name"`
	Model        string    `json:"model"`
	Status       string    `json:"status"`
	WorkDir      string    `json:"work_dir"`
	SDKSessionID string    `json:"sdk_session_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Session status constants.
const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusError     = "error"
	StatusClosed    = "closed"
)
