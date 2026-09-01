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
	ID             string    `json:"id"`
	AgentName      string    `json:"agent_name"`
	Model          string    `json:"model"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	WorkDir        string    `json:"work_dir"`
	StorageRoot    string    `json:"storage_root,omitempty"`
	AgentSessionID string    `json:"agent_session_id"`
	SessionDir     string    `json:"session_dir"`
	ConfigDir      string    `json:"config_dir,omitempty"`
	// SandboxMode is the resolved session sandbox policy (read-only | danger-full-access).
	SandboxMode string `json:"sandbox_mode,omitempty"`
	// FileChangeCollectors is nil for legacy records (Effective → defaults).
	FileChangeCollectors *FileChangeCollectors `json:"file_change_collectors,omitempty"`
	// Usage is the session-level token aggregate (sum of turn totals).
	Usage     *TokenUsage `json:"usage,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// Session status constants.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusCompleted = "completed"
	StatusError     = "error"
	StatusClosed    = "closed"
)
