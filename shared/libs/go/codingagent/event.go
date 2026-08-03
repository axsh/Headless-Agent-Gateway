package codingagent

// EventType represents the type of a streaming event from a coding agent.
type EventType string

const (
	// EventText is a text content event from the agent.
	EventText EventType = "text"
	// EventToolUse is a tool use event from the agent.
	EventToolUse EventType = "tool_use"
	// EventToolResult is a tool result event.
	EventToolResult EventType = "tool_result"
	// EventToolResultPart is a chunk of a large tool_result for SSE wire format.
	EventToolResultPart EventType = "tool_result_part"
	// EventResult is the final result event.
	EventResult EventType = "result"
	// EventError is an error event.
	EventError EventType = "error"
	// EventSystem is a system event (e.g., session init).
	EventSystem EventType = "system"
	// EventNodeStart indicates a WBS node has started execution.
	EventNodeStart EventType = "node_start"
	// EventNodeComplete indicates a WBS node has completed successfully.
	EventNodeComplete EventType = "node_complete"
	// EventNodeFailed indicates a WBS node execution failed.
	EventNodeFailed EventType = "node_failed"
	// EventProgress indicates WBS overall progress (e.g., "2/5").
	EventProgress EventType = "progress"
	// EventUserInputRequired indicates the agent is waiting for user input.
	EventUserInputRequired EventType = "user_input_required"
)

// StreamEvent is a streaming event from a coding agent.
type StreamEvent struct {
	Type       EventType              `json:"type"`
	Content    string                 `json:"content,omitempty"`
	PromptID   string                 `json:"prompt_id,omitempty"`
	Choices    []string               `json:"choices,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolInput  map[string]interface{} `json:"tool_input,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	ChunkID    string                 `json:"chunk_id,omitempty"`
	ChunkIndex int                    `json:"index,omitempty"`
	ChunkTotal int                    `json:"total,omitempty"`
	Error      error                  `json:"-"`
}
