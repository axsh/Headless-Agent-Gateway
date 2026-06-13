package wayfinder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BifrostClient is an LLMClient that communicates with the tern Bifrost proxy
// using the Anthropic messages API format (/v1/messages).
type BifrostClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewBifrostClient creates a new BifrostClient.
func NewBifrostClient(baseURL, token string) *BifrostClient {
	return &BifrostClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// GenerateMessage sends a request to the Bifrost proxy and returns the response.
func (bc *BifrostClient) GenerateMessage(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition) (*LLMResponse, error) {
	body := bc.buildRequestBody(logicalModel, messages, tools)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("bifrost: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("bifrost: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Token", bc.token)

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bifrost: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bifrost: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bifrost: HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var respBody map[string]any
	if err := json.Unmarshal(respBytes, &respBody); err != nil {
		return nil, fmt.Errorf("bifrost: unmarshal response: %w", err)
	}

	return bc.parseResponse(respBody)
}

// buildRequestBody constructs the Anthropic messages API request body.
func (bc *BifrostClient) buildRequestBody(model string, messages []ChatMessage, toolDefs []ToolDefinition) map[string]any {
	body := map[string]any{
		"model":      model,
		"max_tokens": 4096,
	}

	// Convert messages to Anthropic format.
	var apiMessages []map[string]any
	for _, msg := range messages {
		apiMsg := map[string]any{
			"role": msg.Role,
		}

		if msg.Role == "tool" {
			// Tool results use tool_result content block format.
			// Sanitize empty content to prevent upstream API errors (e.g. Gemini HTTP 400).
			content := msg.Content
			if content == "" {
				content = "(no output)"
			}
			apiMsg["role"] = "user"
			apiMsg["content"] = []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": msg.ToolCallID,
					"content":     content,
				},
			}
		} else if len(msg.ToolCalls) > 0 {
			// Assistant message with tool calls.
			var content []map[string]any
			if msg.Content != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": tc.Input,
				})
			}
			apiMsg["content"] = content
		} else {
			// Sanitize empty content to prevent upstream API errors.
			content := msg.Content
			if content == "" {
				content = "(empty)"
			}
			apiMsg["content"] = content
		}

		apiMessages = append(apiMessages, apiMsg)
	}
	body["messages"] = apiMessages

	// Convert tool definitions to Anthropic format.
	if len(toolDefs) > 0 {
		var apiTools []map[string]any
		for _, td := range toolDefs {
			apiTools = append(apiTools, map[string]any{
				"name":         td.Name,
				"description":  td.Description,
				"input_schema": td.InputSchema,
			})
		}
		body["tools"] = apiTools
	}

	return body
}

// parseResponse converts an Anthropic response body to an LLMResponse.
func (bc *BifrostClient) parseResponse(respBody map[string]any) (*LLMResponse, error) {
	result := &LLMResponse{}

	contentArr, ok := respBody["content"].([]any)
	if !ok {
		return result, nil
	}

	var textParts []string
	for _, block := range contentArr {
		blockMap, ok := block.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := blockMap["type"].(string)

		switch blockType {
		case "text":
			text, _ := blockMap["text"].(string)
			textParts = append(textParts, text)
		case "tool_use":
			tc := ToolCall{
				ID:   safeString(blockMap["id"]),
				Name: safeString(blockMap["name"]),
			}
			if input, ok := blockMap["input"].(map[string]any); ok {
				tc.Input = input
			}
			result.ToolCalls = append(result.ToolCalls, tc)
		}
	}

	result.Content = strings.Join(textParts, "\n")
	return result, nil
}

// safeString safely extracts a string from any.
func safeString(v any) string {
	s, _ := v.(string)
	return s
}

// buildStreamRequestBody constructs an Anthropic streaming request body.
func (bc *BifrostClient) buildStreamRequestBody(model string, messages []ChatMessage, toolDefs []ToolDefinition) map[string]any {
	body := bc.buildRequestBody(model, messages, toolDefs)
	body["stream"] = true
	return body
}

// GenerateMessageStream sends a streaming request and calls onDelta for each text delta.
// Returns the final complete response (with tool calls if any) after the stream ends.
func (bc *BifrostClient) GenerateMessageStream(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition, onDelta func(textDelta string)) (*LLMResponse, error) {
	body := bc.buildStreamRequestBody(logicalModel, messages, tools)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("bifrost stream: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bc.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("bifrost stream: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Gateway-Token", bc.token)

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bifrost stream: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bifrost stream: HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	return bc.parseSSEStream(resp.Body, onDelta)
}

// parseSSEStream parses an Anthropic SSE stream, calling onDelta for text deltas
// and collecting tool calls. Returns the final assembled LLMResponse.
func (bc *BifrostClient) parseSSEStream(body io.Reader, onDelta func(textDelta string)) (*LLMResponse, error) {
	scanner := bufio.NewScanner(body)

	var textParts []string
	var toolCalls []ToolCall

	// Track current tool_use block being assembled.
	type pendingTool struct {
		ID       string
		Name     string
		InputBuf strings.Builder
	}
	var currentTool *pendingTool

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "content_block_start":
			// Check if this is a tool_use block.
			cb, _ := event["content_block"].(map[string]any)
			if cb != nil {
				blockType, _ := cb["type"].(string)
				if blockType == "tool_use" {
					currentTool = &pendingTool{
						ID:   safeString(cb["id"]),
						Name: safeString(cb["name"]),
					}
				}
			}

		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			if delta == nil {
				continue
			}
			deltaType, _ := delta["type"].(string)

			switch deltaType {
			case "text_delta":
				text, _ := delta["text"].(string)
				if text != "" {
					textParts = append(textParts, text)
					if onDelta != nil {
						onDelta(text)
					}
				}
			case "input_json_delta":
				// Buffer tool input JSON fragments.
				partial, _ := delta["partial_json"].(string)
				if currentTool != nil && partial != "" {
					currentTool.InputBuf.WriteString(partial)
				}
			}

		case "content_block_stop":
			// Finalize any pending tool_use block.
			if currentTool != nil {
				tc := ToolCall{
					ID:   currentTool.ID,
					Name: currentTool.Name,
				}
				inputJSON := currentTool.InputBuf.String()
				if inputJSON != "" {
					var input map[string]any
					if err := json.Unmarshal([]byte(inputJSON), &input); err == nil {
						tc.Input = input
					}
				}
				toolCalls = append(toolCalls, tc)
				currentTool = nil
			}

		case "message_stop":
			// Stream complete.
		}
	}

	return &LLMResponse{
		Content:   strings.Join(textParts, ""),
		ToolCalls: toolCalls,
	}, nil
}
