package wayfinder

import "context"

// GenerateOptions holds optional parameters for LLM generation.
type GenerateOptions struct {
	ResponseFormat *ResponseFormat
}

// ResponseFormat specifies the desired response format.
type ResponseFormat struct {
	Type       string // "json_object" or "json_schema"
	JSONSchema any    // JSON Schema definition (optional)
}

// ChatMessage represents a single message in the conversation.
type ChatMessage struct {
	Role       string     `json:"role"`                  // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolDefinition describes a tool for the LLM.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// LLMResponse is the response from the LLM.
type LLMResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// LLMClient is the abstract interface for LLM communication.
// Implementations connect to Bifrost/LLMGP or mock for testing.
type LLMClient interface {
	// GenerateMessage sends messages to the specified logical model
	// and returns a response that may contain text and/or tool calls.
	GenerateMessage(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition, opts ...GenerateOptions) (*LLMResponse, error)
}

// StreamingLLMClient extends LLMClient with streaming support.
type StreamingLLMClient interface {
	LLMClient
	// GenerateMessageStream sends a streaming request and calls onDelta
	// for each text delta chunk. Returns the final complete response
	// (including any tool calls) after the stream ends.
	GenerateMessageStream(
		ctx context.Context,
		logicalModel string,
		messages []ChatMessage,
		tools []ToolDefinition,
		onDelta func(textDelta string),
	) (*LLMResponse, error)
}
