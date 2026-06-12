package session

import (
	"encoding/json"
	"time"
)

// Session status constants.
const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Message represents a conversation message with metadata for compaction.
type Message struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Timestamp  time.Time        `json:"timestamp"`
	Pinned     bool             `json:"pinned"`
	ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// ToolCallRecord records a tool call within a message.
type ToolCallRecord struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// TrackedFile represents a file created by the agent (deletion permission list entry).
type TrackedFile struct {
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	IsDir     bool      `json:"is_dir"`
}

// TrackedProcess represents a background process launched by the agent.
type TrackedProcess struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	Args      []string  `json:"args"`
	StartedAt time.Time `json:"started_at"`
}

// SessionState is the full serializable session state.
type SessionState struct {
	SessionID        string           `json:"session_id"`
	ParentID         *string          `json:"parent_id,omitempty"`
	Status           string           `json:"status"`
	Messages         []Message        `json:"messages"`
	CreatedFiles     []TrackedFile    `json:"created_files"`
	RunningProcesses []TrackedProcess `json:"running_processes"`
	WBSTreeJSON      json.RawMessage  `json:"wbs_tree,omitempty"` // Serialized planning.WBSTree
	CreatedAt        time.Time        `json:"created_at"`
	LastActivityAt   time.Time        `json:"last_activity_at"`
}
