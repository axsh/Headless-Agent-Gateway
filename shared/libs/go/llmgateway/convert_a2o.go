package llmgateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- Anthropic Types ---

// AnthropicFullRequest represents the full Anthropic Messages API request body.
type AnthropicFullRequest struct {
	Model       string          `json:"model"`
	Messages    []AnthropicMsg  `json:"messages"`
	System      json.RawMessage `json:"system,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stream      *bool           `json:"stream,omitempty"`
}

// AnthropicMsg represents a message in Anthropic format.
type AnthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock represents a content block in Anthropic format.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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
}

// OpenAIMsg represents a message in OpenAI format.
type OpenAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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

	if req.MaxTokens > 0 {
		mt := req.MaxTokens
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

	// Convert each Anthropic message to OpenAI message.
	for i, msg := range req.Messages {
		content, err := extractText(msg.Content)
		if err != nil {
			return nil, fmt.Errorf("parse message[%d] content: %w", i, err)
		}
		oaiReq.Messages = append(oaiReq.Messages, OpenAIMsg{
			Role:    msg.Role,
			Content: content,
		})
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
			resp.Content = []ContentBlock{
				{Type: "text", Text: choice.Message.Content},
			}
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
	default:
		return reason
	}
}
