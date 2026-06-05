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
	// EventResult is the final result event.
	EventResult EventType = "result"
	// EventError is an error event.
	EventError EventType = "error"
	// EventSystem is a system event (e.g., session init).
	EventSystem EventType = "system"
)

// StreamEvent is a streaming event from a coding agent.
type StreamEvent struct {
	Type      EventType              `json:"type"`
	Content   string                 `json:"content,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	Error     error                  `json:"-"`
}
