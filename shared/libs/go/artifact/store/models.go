// Package store provides persistence for Tern artifact events.
package store

import "time"

// Operation constants for artifact events.
const (
	OperationCreate = "create"
	OperationUpdate = "update"
	OperationDelete = "delete"
)

// Session represents a Coding Agent session record.
type Session struct {
	ID        string
	AgentID   string
	AgentName string
	StartedAt time.Time
	EndedAt   *time.Time
}

// SystemArtifactEvent represents one file-operation event produced by a Coding Agent tool call.
type SystemArtifactEvent struct {
	ID         int64
	SessionID  string
	AgentID    string
	Key        string // project-root-relative logical path
	ActualPath string // absolute path on the file system
	Operation  string // "create" | "update" | "delete"
	OccurredAt time.Time
	ToolName   string
	ContentSHA string // SHA256 hex; may be empty
}

// SystemArtifactFilter specifies filters and pagination for ListSystemArtifacts.
type SystemArtifactFilter struct {
	Q              string   // doublestar glob applied to Key
	AgentIDs       []string // filter by agent ID (OR)
	SessionIDs     []string // filter by session ID (OR)
	Operation      string   // "" means all
	Since          *time.Time
	Until          *time.Time
	IncludeDeleted bool // if false, exclude keys whose latest operation is "delete"
	Page           int  // 1-indexed; 0 treated as 1
	PerPage        int  // default 30, max 100; 0 treated as 30
	Sort           string // "key" | "occurred_at" | "operation"
	Order          string // "asc" | "desc"
}

// SystemArtifactPage is the paginated result of ListSystemArtifacts.
type SystemArtifactPage struct {
	TotalCount int
	Page       int
	PerPage    int
	Items      []SystemArtifactEvent
}

// UserArtifact is a user-uploaded artifact stored in Tern-managed storage.
type UserArtifact struct {
	ID         string // UUID
	Key        string // user-defined logical path (unique)
	ActualPath string // path inside Tern-managed storage
	Filename   string // original upload filename
	Size       int64
	MIMEType   string
	ContentSHA string // SHA256 hex
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// UserArtifactFilter specifies filters and pagination for ListUserArtifacts.
type UserArtifactFilter struct {
	Q       string // doublestar glob applied to Key
	Page    int    // 1-indexed
	PerPage int    // default 30, max 100
	Sort    string // "key" | "created_at" | "updated_at" | "size"
	Order   string // "asc" | "desc"
}

// UserArtifactPage is the paginated result of ListUserArtifacts.
type UserArtifactPage struct {
	TotalCount int
	Page       int
	PerPage    int
	Items      []UserArtifact
}
