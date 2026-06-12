package wayfinder

import (
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
	req.Header.Set("Authorization", "Bearer "+bc.token)

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
			apiMsg["role"] = "user"
			apiMsg["content"] = []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": msg.ToolCallID,
					"content":     msg.Content,
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
			apiMsg["content"] = msg.Content
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
