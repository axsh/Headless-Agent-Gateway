package llmgateway

import "encoding/json"

// --- Anthropic Types ---
// These types define the Anthropic Messages API request/response format.
// They are used by the Bifrost conversion layer (convert_anthropic_bifrost.go)
// and the fallback/tool_call_fallback logic.

// AnthropicFullRequest represents the full Anthropic Messages API request body.
type AnthropicFullRequest struct {
	Model       string          `json:"model"`
	Messages    []AnthropicMsg  `json:"messages"`
	System      json.RawMessage `json:"system,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stream      *bool           `json:"stream,omitempty"`
	Tools       []AnthropicTool `json:"tools,omitempty"`
}

// AnthropicTool represents a tool definition in Anthropic format.
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// AnthropicMsg represents a message in Anthropic format.
type AnthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock represents a content block in Anthropic format.
// It supports text, tool_use, and tool_result block types.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

// AnthropicResponse represents the Anthropic Messages API response.
type AnthropicResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      AnthropicUsage `json:"usage"`
}

// AnthropicUsage represents token usage in Anthropic format.
type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
