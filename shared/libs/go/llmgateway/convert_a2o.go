package llmgateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// openAIMaxCompletionTokens is the safe default max completion tokens for OpenAI models.
// Most OpenAI models support at least 16384 completion tokens.
// Claude CLI typically sends max_tokens=32000 which exceeds this limit.
const openAIMaxCompletionTokens = 16384

// --- Anthropic Types ---

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

// --- OpenAI Types ---

// OpenAIRequest represents the OpenAI Chat Completions API request body.
type OpenAIRequest struct {
	Model       string      `json:"model"`
	Messages    []OpenAIMsg `json:"messages"`
	MaxTokens   *int        `json:"max_tokens,omitempty"`
	Temperature *float64    `json:"temperature,omitempty"`
	Stream      *bool       `json:"stream,omitempty"`
	Tools       []OpenAITool `json:"tools,omitempty"`
}

// OpenAIMsg represents a message in OpenAI format.
type OpenAIMsg struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []OpenAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// OpenAIResponse represents the OpenAI Chat Completions API response.
type OpenAIResponse struct {
	ID      string         `json:"id"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

// OpenAIChoice represents a choice in OpenAI response.
type OpenAIChoice struct {
	Message      OpenAIMsg `json:"message"`
	FinishReason string    `json:"finish_reason"`
}

// OpenAITool represents a tool definition in OpenAI format.
type OpenAITool struct {
	Type     string         `json:"type"`
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction represents a function definition in OpenAI format.
type OpenAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// OpenAIToolCall represents a tool call in OpenAI response.
type OpenAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function OpenAIFuncCall `json:"function"`
}

// OpenAIFuncCall represents a function call in OpenAI response.
type OpenAIFuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAIUsage represents token usage in OpenAI format.
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// --- Conversion Functions ---

// ConvertAnthropicRequestToOpenAI converts an Anthropic Messages API request body
// to an OpenAI Chat Completions API request body.
func ConvertAnthropicRequestToOpenAI(body []byte) ([]byte, error) {
	var req AnthropicFullRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse anthropic request: %w", err)
	}

	oaiReq := OpenAIRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		Stream:      req.Stream,
	}

	// Clamp max_tokens for OpenAI compatibility.
	// Claude CLI often sends max_tokens=32000 which exceeds limits for many OpenAI models
	// (e.g., gpt-4o supports max 16384). We clamp to a safe default.
	if req.MaxTokens > 0 {
		mt := req.MaxTokens
		if mt > openAIMaxCompletionTokens {
			mt = openAIMaxCompletionTokens
		}
		oaiReq.MaxTokens = &mt
	}

	// Convert system field to a system message at the front.
	if len(req.System) > 0 {
		systemText, err := extractText(req.System)
		if err != nil {
			return nil, fmt.Errorf("parse system field: %w", err)
		}
		if systemText != "" {
			oaiReq.Messages = append(oaiReq.Messages, OpenAIMsg{
				Role:    "system",
				Content: systemText,
			})
		}
	}

	// Convert tools if present.
	for _, tool := range req.Tools {
		oaiReq.Tools = append(oaiReq.Tools, OpenAITool{
			Type: "function",
			Function: OpenAIFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	// Convert each Anthropic message to OpenAI message.
	for i, msg := range req.Messages {
		converted, err := convertAnthropicMsg(i, msg)
		if err != nil {
			return nil, err
		}
		oaiReq.Messages = append(oaiReq.Messages, converted...)
	}

	return json.Marshal(oaiReq)
}

// ConvertOpenAIResponseToAnthropic converts an OpenAI Chat Completions API response body
// to an Anthropic Messages API response body.
func ConvertOpenAIResponseToAnthropic(body []byte, model string) ([]byte, error) {
	var oaiResp OpenAIResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}

	resp := AnthropicResponse{
		ID:    oaiResp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Usage: AnthropicUsage{
			InputTokens:  oaiResp.Usage.PromptTokens,
			OutputTokens: oaiResp.Usage.CompletionTokens,
		},
	}

	if len(oaiResp.Choices) > 0 {
		choice := oaiResp.Choices[0]
		if choice.Message.Content != "" {
			resp.Content = append(resp.Content, ContentBlock{
				Type: "text", Text: choice.Message.Content,
			})
		}
		// Convert tool_calls to tool_use content blocks.
		for _, tc := range choice.Message.ToolCalls {
			resp.Content = append(resp.Content, ContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			})
		}
		resp.StopReason = mapFinishReason(choice.FinishReason)
	}

	return json.Marshal(resp)
}

// extractText extracts text from a json.RawMessage that is either a plain string
// or an array of ContentBlocks.
func extractText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	// Try as plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	// Try as array of content blocks.
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("content is neither string nor []ContentBlock: %s", string(raw))
	}

	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, ""), nil
}

// mapFinishReason maps OpenAI finish_reason to Anthropic stop_reason.
func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

// convertAnthropicMsg converts one Anthropic message to one or more OpenAI messages.
// tool_use blocks become assistant messages with tool_calls.
// tool_result blocks become tool-role messages.
func convertAnthropicMsg(idx int, msg AnthropicMsg) ([]OpenAIMsg, error) {
	// Try as plain string first.
	var plainStr string
	if err := json.Unmarshal(msg.Content, &plainStr); err == nil {
		return []OpenAIMsg{{Role: msg.Role, Content: plainStr}}, nil
	}

	// Parse as array of content blocks.
	var blocks []json.RawMessage
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("parse message[%d] content: %w", idx, err)
	}

	// Classify blocks.
	var textParts []string
	var toolUses []OpenAIToolCall
	var toolResults []OpenAIMsg

	for _, rawBlock := range blocks {
		var block struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			Content   string          `json:"content"`
		}
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("parse message[%d] block: %w", idx, err)
		}

		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			args, _ := json.Marshal(json.RawMessage(block.Input))
			toolUses = append(toolUses, OpenAIToolCall{
				ID:   block.ID,
				Type: "function",
				Function: OpenAIFuncCall{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		case "tool_result":
			toolResults = append(toolResults, OpenAIMsg{
				Role:       "tool",
				Content:    block.Content,
				ToolCallID: block.ToolUseID,
			})
		}
	}

	var result []OpenAIMsg

	// If there are tool_result blocks, they become separate tool messages.
	if len(toolResults) > 0 {
		result = append(result, toolResults...)
		return result, nil
	}

	// Otherwise emit a single message with text and/or tool_calls.
	oaiMsg := OpenAIMsg{
		Role:    msg.Role,
		Content: strings.Join(textParts, ""),
	}
	if len(toolUses) > 0 {
		oaiMsg.ToolCalls = toolUses
	}
	result = append(result, oaiMsg)
	return result, nil
}
