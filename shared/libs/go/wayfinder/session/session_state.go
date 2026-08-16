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
	StatusSuspended = "suspended" // Awaiting user feedback via ask_user tool.
)

// Origin values match coding-agent adapter Name() values.
const (
	OriginClaudeCode = "claudecode"
	OriginCodex      = "codex"
	OriginWayfinder  = "wayfinder"
)

// NormalizeOrigin returns a known origin, defaulting unknown/empty values to wayfinder.
func NormalizeOrigin(origin string) string {
	switch origin {
	case OriginClaudeCode, OriginCodex, OriginWayfinder:
		return origin
	default:
		return OriginWayfinder
	}
}

// Message represents a conversation message with metadata for compaction.
type Message struct {
	Role         string           `json:"role"`
	Content      string           `json:"content"`
	ContentParts []ContentPart    `json:"content_parts,omitempty"`
	Timestamp    time.Time        `json:"timestamp"`
	Pinned       bool             `json:"pinned"`
	Seq          int              `json:"seq"` // Global sequence number (immutable after assignment).
	ToolCalls    []ToolCallRecord `json:"tool_calls,omitempty"`
	ToolCallID   string           `json:"tool_call_id,omitempty"`
	Origin       string           `json:"origin,omitempty"`
}

// ContentPart represents a part of a message content, allowing for multimodal data.
type ContentPart struct {
	Type  string         `json:"type"` // "text", "image"
	Text  string         `json:"text,omitempty"`
	Image *ImageMetadata `json:"image,omitempty"`
}

// ImageMetadata represents metadata for an image stored in the session directory.
type ImageMetadata struct {
	Path      string `json:"path"`       // Relative path from the session directory.
	MediaType string `json:"media_type"` // e.g., "image/png"
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
	SessionID        string                  `json:"session_id"`
	ParentID         *string                 `json:"parent_id,omitempty"`
	Status           string                  `json:"status"`
	Messages         []Message               `json:"messages"`
	CreatedFiles     []TrackedFile           `json:"created_files"`
	RunningProcesses []TrackedProcess        `json:"running_processes"`
	WBSTreeJSON      json.RawMessage         `json:"wbs_tree,omitempty"` // Serialized planning.WBSTree
	CreatedAt        time.Time               `json:"created_at"`
	LastActivityAt   time.Time               `json:"last_activity_at"`
	ActiveAgent      string                  `json:"active_agent,omitempty"`
	AgentBindings    map[string]AgentBinding `json:"agent_bindings,omitempty"`
	Supplement       SupplementStrategy      `json:"supplement,omitempty"`
}

// AgentBinding records a coding agent's native session id and ingest watermark.
type AgentBinding struct {
	AgentSessionID     string `json:"agent_session_id"`
	IngestedThroughSeq int    `json:"ingested_through_seq"`
}

// SupplementStrategy is the per-session context-transfer policy (applied in portable).
type SupplementStrategy struct {
	Algorithm        string `json:"algorithm,omitempty"`
	Model            string `json:"model,omitempty"`
	MaxChunkMessages int    `json:"max_chunk_messages,omitempty"`
	ThresholdBytes   int    `json:"threshold_bytes,omitempty"`
	RecentKeep       int    `json:"recent_keep,omitempty"`
}

// SessionMetadata is persisted as metadata.json in the session folder.
// It holds session-level metadata separately from the message context.
type SessionMetadata struct {
	SessionID        string                  `json:"session_id"`
	ParentID         *string                 `json:"parent_id,omitempty"`
	Status           string                  `json:"status"`
	Latest           int                     `json:"latest"`        // Last history sequence number (legacy, kept for backward compat).
	TotalSeq         int                     `json:"total_seq"`     // Max global sequence number across all messages.
	ContextStart     int                     `json:"context_start"` // First seq in current context (before = summarized).
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
	WBSTreeJSON      json.RawMessage         `json:"wbs_tree,omitempty"`
	CreatedFiles     []TrackedFile           `json:"created_files"`
	RunningProcesses []TrackedProcess        `json:"running_processes"`
	ActiveAgent      string                  `json:"active_agent,omitempty"`
	AgentBindings    map[string]AgentBinding `json:"agent_bindings,omitempty"`
	Supplement       SupplementStrategy      `json:"supplement,omitempty"`
}
