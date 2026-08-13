package codingagent

import (
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

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
	ID             string                                  `json:"id"`
	AgentName      string                                  `json:"agent_name"`
	Model          string                                  `json:"model"`
	Status         string                                  `json:"status"`
	Error          string                                  `json:"error,omitempty"`
	WorkDir        string                                  `json:"work_dir"`
	AgentSessionID string                                  `json:"agent_session_id"`
	SessionDir     string                                  `json:"session_dir"`
	ConfigDir      string                                  `json:"config_dir,omitempty"`
	MCPServers     map[string]toolconfig.MCPServerConfig   `json:"mcp_servers,omitempty"`
	Functions      map[string]toolconfig.FunctionConfig    `json:"functions,omitempty"`
	CreatedAt      time.Time                               `json:"created_at"`
	UpdatedAt      time.Time                               `json:"updated_at"`
}

// Session status constants.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusCompleted = "completed"
	StatusError     = "error"
	StatusClosed    = "closed"
)
