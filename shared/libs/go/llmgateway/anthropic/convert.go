package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

// ConvertToBifrost converts an Anthropic Messages API request
// to a BifrostResponsesRequest for provider-agnostic routing via Bifrost SDK.
func ConvertToBifrost(
	req *FullRequest,
	provider bifrostSchemas.ModelProvider,
) (*bifrostSchemas.BifrostResponsesRequest, error) {
	bifrostReq := &bifrostSchemas.BifrostResponsesRequest{
		Provider: provider,
		Model:    req.Model,
		Params:   &bifrostSchemas.ResponsesParameters{},
	}

	// 1. System -> Instructions
	if req.System != nil && len(req.System) > 0 {
		instructions, err := extractSystemInstructions(req.System)
		if err != nil {
			return nil, fmt.Errorf("failed to parse system message: %w", err)
		}
		bifrostReq.Params.Instructions = &instructions
	}

	// 2. Messages -> Input
	for _, msg := range req.Messages {
		converted, err := convertMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		bifrostReq.Input = append(bifrostReq.Input, converted...)
	}

	// 3. Tools -> Params.Tools
	for _, tool := range req.Tools {
		bifrostReq.Params.Tools = append(bifrostReq.Params.Tools,
			convertTool(tool))
	}

	// 4. Parameters
	if req.MaxTokens > 0 {
		bifrostReq.Params.MaxOutputTokens = &req.MaxTokens
	}
	if req.Temperature != nil {
		bifrostReq.Params.Temperature = req.Temperature
	}

	return bifrostReq, nil
}

// extractSystemInstructions extracts system instructions from Anthropic's system field.
// The system field can be either a plain string or an array of content blocks.
func extractSystemInstructions(raw json.RawMessage) (string, error) {
	// Try string first
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, nil
	}

	// Try array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("system must be a string or array of content blocks: %w", err)
	}

	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

// convertMessage converts a single Anthropic message to one or more
// Bifrost ResponsesMessage entries. A single Anthropic message with mixed
// content blocks (text + tool_use) may expand into multiple Bifrost messages.
func convertMessage(msg Message) ([]bifrostSchemas.ResponsesMessage, error) {
	// Try string content first
	var textContent string
	if err := json.Unmarshal(msg.Content, &textContent); err == nil {
		role := toBifrostRole(msg.Role)
		return []bifrostSchemas.ResponsesMessage{
			{
				Role: &role,
				Content: &bifrostSchemas.ResponsesMessageContent{
					ContentStr: &textContent,
				},
			},
		}, nil
	}

	// Parse as array of content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("content must be a string or array of content blocks: %w", err)
	}

	var result []bifrostSchemas.ResponsesMessage

	// Group text blocks into a single message, emit tool_use/tool_result as separate messages
	var textParts []string

	for _, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)

		case "tool_use":
			// Flush accumulated text first
			if len(textParts) > 0 {
				role := toBifrostRole(msg.Role)
				combined := strings.Join(textParts, "")
				result = append(result, bifrostSchemas.ResponsesMessage{
					Role: &role,
					Content: &bifrostSchemas.ResponsesMessageContent{
						ContentStr: &combined,
					},
				})
				textParts = nil
			}

			// Convert tool_use -> function_call
			argsStr := string(block.Input)
			callID := block.ID
			name := block.Name
			msgType := bifrostSchemas.ResponsesMessageTypeFunctionCall
			result = append(result, bifrostSchemas.ResponsesMessage{
				Type: &msgType,
				ResponsesToolMessage: &bifrostSchemas.ResponsesToolMessage{
					CallID:    &callID,
					Name:      &name,
					Arguments: &argsStr,
				},
			})

		case "tool_result":
			// Flush accumulated text first
			if len(textParts) > 0 {
				role := toBifrostRole(msg.Role)
				combined := strings.Join(textParts, "")
				result = append(result, bifrostSchemas.ResponsesMessage{
					Role: &role,
					Content: &bifrostSchemas.ResponsesMessageContent{
						ContentStr: &combined,
					},
				})
				textParts = nil
			}

			// Convert tool_result -> function_call_output
			callID := block.ToolUseID
			output := block.Content
			msgType := bifrostSchemas.ResponsesMessageTypeFunctionCallOutput
			result = append(result, bifrostSchemas.ResponsesMessage{
				Type: &msgType,
				ResponsesToolMessage: &bifrostSchemas.ResponsesToolMessage{
					CallID: &callID,
					Output: &bifrostSchemas.ResponsesToolMessageOutputStruct{
						ResponsesToolCallOutputStr: &output,
					},
				},
			})
		}
	}

	// Flush remaining text
	if len(textParts) > 0 {
		role := toBifrostRole(msg.Role)
		combined := strings.Join(textParts, "")
		result = append(result, bifrostSchemas.ResponsesMessage{
			Role: &role,
			Content: &bifrostSchemas.ResponsesMessageContent{
				ContentStr: &combined,
			},
		})
	}

	return result, nil
}

// convertTool converts an Anthropic tool definition to a Bifrost ResponsesTool.
func convertTool(tool Tool) bifrostSchemas.ResponsesTool {
	name := tool.Name
	desc := tool.Description

	// Convert InputSchema (json.RawMessage) to ToolFunctionParameters
	var params *bifrostSchemas.ToolFunctionParameters
	if len(tool.InputSchema) > 0 {
		var tfp bifrostSchemas.ToolFunctionParameters
		if err := json.Unmarshal(tool.InputSchema, &tfp); err == nil {
			params = &tfp
		}
	}

	return bifrostSchemas.ResponsesTool{
		Type:        bifrostSchemas.ResponsesToolTypeFunction,
		Name:        &name,
		Description: &desc,
		ResponsesToolFunction: &bifrostSchemas.ResponsesToolFunction{
			Parameters: params,
		},
	}
}

// toBifrostRole converts an Anthropic role string to a Bifrost ResponsesMessageRoleType.
func toBifrostRole(role string) bifrostSchemas.ResponsesMessageRoleType {
	switch role {
	case "user":
		return bifrostSchemas.ResponsesInputMessageRoleUser
	case "assistant":
		return bifrostSchemas.ResponsesInputMessageRoleAssistant
	case "system":
		return bifrostSchemas.ResponsesInputMessageRoleSystem
	default:
		return bifrostSchemas.ResponsesMessageRoleType(role)
	}
}

// --- Reverse Conversion: Bifrost -> Anthropic ---

// ConvertFromBifrost converts a BifrostResponsesResponse
// to an Anthropic Messages API response.
func ConvertFromBifrost(
	resp *bifrostSchemas.BifrostResponsesResponse,
) (*Response, error) {
	anthResp := &Response{
		ID:    generateID(),
		Type:  "message",
		Role:  "assistant",
		Model: resp.Model,
	}

	// 1. Output -> Content blocks
	for _, msg := range resp.Output {
		blocks, err := convertBifrostOutputToContentBlocks(msg)
		if err != nil {
			return nil, err
		}
		anthResp.Content = append(anthResp.Content, blocks...)
	}

	// Ensure Content is never nil (Anthropic API expects empty array, not null)
	if anthResp.Content == nil {
		anthResp.Content = []ContentBlock{}
	}

	// 2. StopReason
	anthResp.StopReason = mapBifrostStopReason(resp)

	// 3. Usage
	if resp.Usage != nil {
		anthResp.Usage = Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		}
	}

	return anthResp, nil
}

// convertBifrostOutputToContentBlocks converts a single Bifrost output message
// to Anthropic content blocks.
func convertBifrostOutputToContentBlocks(msg bifrostSchemas.ResponsesMessage) ([]ContentBlock, error) {
	// Check for function_call (tool_use in Anthropic)
	if msg.Type != nil && *msg.Type == bifrostSchemas.ResponsesMessageTypeFunctionCall {
		if msg.ResponsesToolMessage != nil {
			block := ContentBlock{
				Type: "tool_use",
			}
			if msg.ResponsesToolMessage.CallID != nil {
				block.ID = *msg.ResponsesToolMessage.CallID
			}
			if msg.ResponsesToolMessage.Name != nil {
				block.Name = *msg.ResponsesToolMessage.Name
			}
			if msg.ResponsesToolMessage.Arguments != nil {
				block.Input = json.RawMessage(*msg.ResponsesToolMessage.Arguments)
			}
			return []ContentBlock{block}, nil
		}
	}

	// Text content
	if msg.Content != nil {
		if msg.Content.ContentStr != nil {
			return []ContentBlock{
				{Type: "text", Text: *msg.Content.ContentStr},
			}, nil
		}
		// Content blocks from Bifrost
		if msg.Content.ContentBlocks != nil {
			var blocks []ContentBlock
			for _, cb := range msg.Content.ContentBlocks {
				if cb.Text != nil {
					blocks = append(blocks, ContentBlock{
						Type: "text",
						Text: *cb.Text,
					})
				}
			}
			return blocks, nil
		}
	}

	return nil, nil
}

// mapBifrostStopReason converts Bifrost stop_reason to Anthropic stop_reason.
func mapBifrostStopReason(resp *bifrostSchemas.BifrostResponsesResponse) string {
	if resp.StopReason != nil {
		switch *resp.StopReason {
		case "tool_use":
			return "tool_use"
		case "max_tokens":
			return "max_tokens"
		case "stop", "end_turn", "":
			return "end_turn"
		default:
			return "end_turn"
		}
	}
	return "end_turn"
}

// generateID generates a message ID in the Anthropic format.
func generateID() string {
	return "msg_bifrost_" + randomHexString(12)
}

// randomHexString generates a random hex string of the given length.
func randomHexString(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[i%len(hexChars)]
	}
	return string(b)
}
