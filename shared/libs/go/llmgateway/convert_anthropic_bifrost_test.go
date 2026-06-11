package llmgateway

import (
	"encoding/json"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Forward Conversion Tests: Anthropic -> Bifrost ---

func TestConvertAnthropicToBifrost_BasicMessage(t *testing.T) {
	req := &AnthropicFullRequest{
		Model:     "claude-3-sonnet",
		MaxTokens: 4096,
		System:    json.RawMessage(`"You are a helpful assistant."`),
		Messages: []AnthropicMsg{
			{Role: "user", Content: json.RawMessage(`"Hello, how are you?"`)},
		},
	}

	result, err := ConvertAnthropicToBifrost(req, bifrostSchemas.Anthropic)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, bifrostSchemas.Anthropic, result.Provider)
	assert.Equal(t, "claude-3-sonnet", result.Model)

	// System -> Instructions
	require.NotNil(t, result.Params)
	require.NotNil(t, result.Params.Instructions)
	assert.Equal(t, "You are a helpful assistant.", *result.Params.Instructions)

	// Messages -> Input
	require.Len(t, result.Input, 1)
	require.NotNil(t, result.Input[0].Role)
	assert.Equal(t, bifrostSchemas.ResponsesInputMessageRoleUser, *result.Input[0].Role)
	require.NotNil(t, result.Input[0].Content)
	require.NotNil(t, result.Input[0].Content.ContentStr)
	assert.Equal(t, "Hello, how are you?", *result.Input[0].Content.ContentStr)
}

func TestConvertAnthropicToBifrost_SystemContentBlocks(t *testing.T) {
	req := &AnthropicFullRequest{
		Model:     "claude-3-sonnet",
		MaxTokens: 1024,
		System:    json.RawMessage(`[{"type":"text","text":"First part."},{"type":"text","text":"Second part."}]`),
		Messages: []AnthropicMsg{
			{Role: "user", Content: json.RawMessage(`"test"`)},
		},
	}

	result, err := ConvertAnthropicToBifrost(req, bifrostSchemas.Anthropic)
	require.NoError(t, err)
	require.NotNil(t, result.Params)
	require.NotNil(t, result.Params.Instructions)
	assert.Equal(t, "First part.\nSecond part.", *result.Params.Instructions)
}

func TestConvertAnthropicToBifrost_ToolUse(t *testing.T) {
	// Assistant message with tool_use content block
	toolInput := json.RawMessage(`{"path":"/tmp/test.txt","content":"hello"}`)
	assistantContent, _ := json.Marshal([]ContentBlock{
		{Type: "text", Text: "I'll write the file."},
		{Type: "tool_use", ID: "call_123", Name: "write_file", Input: toolInput},
	})

	// User message with tool_result
	toolResultContent, _ := json.Marshal([]ContentBlock{
		{Type: "tool_result", ToolUseID: "call_123", Content: "File written successfully"},
	})

	req := &AnthropicFullRequest{
		Model:     "claude-3-sonnet",
		MaxTokens: 4096,
		Messages: []AnthropicMsg{
			{Role: "user", Content: json.RawMessage(`"Write a file"`)},
			{Role: "assistant", Content: assistantContent},
			{Role: "user", Content: toolResultContent},
		},
	}

	result, err := ConvertAnthropicToBifrost(req, bifrostSchemas.OpenAI)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Expect: user message, assistant text, function_call, function_call_output
	require.GreaterOrEqual(t, len(result.Input), 3)

	// Check function_call message
	var foundFunctionCall, foundFunctionCallOutput bool
	for _, msg := range result.Input {
		if msg.Type != nil {
			switch *msg.Type {
			case bifrostSchemas.ResponsesMessageTypeFunctionCall:
				foundFunctionCall = true
				require.NotNil(t, msg.ResponsesToolMessage)
				assert.Equal(t, "call_123", *msg.ResponsesToolMessage.CallID)
				assert.Equal(t, "write_file", *msg.ResponsesToolMessage.Name)
			case bifrostSchemas.ResponsesMessageTypeFunctionCallOutput:
				foundFunctionCallOutput = true
				require.NotNil(t, msg.ResponsesToolMessage)
				assert.Equal(t, "call_123", *msg.ResponsesToolMessage.CallID)
			}
		}
	}
	assert.True(t, foundFunctionCall, "expected function_call message in Input")
	assert.True(t, foundFunctionCallOutput, "expected function_call_output message in Input")
}

func TestConvertAnthropicToBifrost_Tools(t *testing.T) {
	req := &AnthropicFullRequest{
		Model:     "claude-3-sonnet",
		MaxTokens: 4096,
		Tools: []AnthropicTool{
			{
				Name:        "read_file",
				Description: "Read a file from disk",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
			},
		},
		Messages: []AnthropicMsg{
			{Role: "user", Content: json.RawMessage(`"test"`)},
		},
	}

	result, err := ConvertAnthropicToBifrost(req, bifrostSchemas.OpenAI)
	require.NoError(t, err)
	require.NotNil(t, result.Params)
	require.Len(t, result.Params.Tools, 1)

	tool := result.Params.Tools[0]
	assert.Equal(t, bifrostSchemas.ResponsesToolTypeFunction, tool.Type)
	require.NotNil(t, tool.Name)
	assert.Equal(t, "read_file", *tool.Name)
	require.NotNil(t, tool.Description)
	assert.Equal(t, "Read a file from disk", *tool.Description)
	require.NotNil(t, tool.ResponsesToolFunction)
	require.NotNil(t, tool.ResponsesToolFunction.Parameters)
}

func TestConvertAnthropicToBifrost_Parameters(t *testing.T) {
	temp := 0.7
	req := &AnthropicFullRequest{
		Model:       "claude-3-sonnet",
		MaxTokens:   8192,
		Temperature: &temp,
		Messages: []AnthropicMsg{
			{Role: "user", Content: json.RawMessage(`"test"`)},
		},
	}

	result, err := ConvertAnthropicToBifrost(req, bifrostSchemas.Anthropic)
	require.NoError(t, err)
	require.NotNil(t, result.Params)

	require.NotNil(t, result.Params.MaxOutputTokens)
	assert.Equal(t, 8192, *result.Params.MaxOutputTokens)

	require.NotNil(t, result.Params.Temperature)
	assert.Equal(t, 0.7, *result.Params.Temperature)
}

func TestConvertAnthropicToBifrost_StreamFlag(t *testing.T) {
	// Stream flag should not affect the conversion
	// (streaming is handled at handler level)
	stream := true
	req := &AnthropicFullRequest{
		Model:     "claude-3-sonnet",
		MaxTokens: 1024,
		Stream:    &stream,
		Messages: []AnthropicMsg{
			{Role: "user", Content: json.RawMessage(`"test"`)},
		},
	}

	result, err := ConvertAnthropicToBifrost(req, bifrostSchemas.Anthropic)
	require.NoError(t, err)
	require.NotNil(t, result)
	// No stream field in BifrostResponsesRequest
	// Just verify the conversion succeeds
	assert.Equal(t, "claude-3-sonnet", result.Model)
}

// --- Reverse Conversion Tests: Bifrost -> Anthropic ---

func TestConvertBifrostToAnthropic_BasicResponse(t *testing.T) {
	textContent := "Hello! I'm doing well."
	resp := &bifrostSchemas.BifrostResponsesResponse{
		Model:  "claude-3-sonnet",
		Output: []bifrostSchemas.ResponsesMessage{
			{
				Role: ptr(bifrostSchemas.ResponsesInputMessageRoleAssistant),
				Content: &bifrostSchemas.ResponsesMessageContent{
					ContentStr: &textContent,
				},
			},
		},
		Usage: &bifrostSchemas.ResponsesResponseUsage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	result, err := ConvertBifrostToAnthropic(resp)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "message", result.Type)
	assert.Equal(t, "assistant", result.Role)
	assert.Equal(t, "claude-3-sonnet", result.Model)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "Hello! I'm doing well.", result.Content[0].Text)
	assert.Equal(t, 100, result.Usage.InputTokens)
	assert.Equal(t, 50, result.Usage.OutputTokens)
}

func TestConvertBifrostToAnthropic_ToolUseOutput(t *testing.T) {
	callID := "call_abc"
	name := "read_file"
	args := `{"path":"/tmp/test.txt"}`
	funcCallType := bifrostSchemas.ResponsesMessageTypeFunctionCall
	resp := &bifrostSchemas.BifrostResponsesResponse{
		Model: "claude-3-sonnet",
		Output: []bifrostSchemas.ResponsesMessage{
			{
				Type: &funcCallType,
				ResponsesToolMessage: &bifrostSchemas.ResponsesToolMessage{
					CallID:    &callID,
					Name:      &name,
					Arguments: &args,
				},
			},
		},
	}

	result, err := ConvertBifrostToAnthropic(resp)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "tool_use", result.Content[0].Type)
	assert.Equal(t, "call_abc", result.Content[0].ID)
	assert.Equal(t, "read_file", result.Content[0].Name)
	assert.JSONEq(t, `{"path":"/tmp/test.txt"}`, string(result.Content[0].Input))
}

func TestConvertBifrostToAnthropic_Usage(t *testing.T) {
	resp := &bifrostSchemas.BifrostResponsesResponse{
		Model: "claude-3-sonnet",
		Usage: &bifrostSchemas.ResponsesResponseUsage{
			InputTokens:  250,
			OutputTokens: 120,
		},
	}

	result, err := ConvertBifrostToAnthropic(resp)
	require.NoError(t, err)
	assert.Equal(t, 250, result.Usage.InputTokens)
	assert.Equal(t, 120, result.Usage.OutputTokens)
}

func TestConvertBifrostToAnthropic_StopReason(t *testing.T) {
	tests := []struct {
		name       string
		stopReason *string
		expected   string
	}{
		{
			name:       "nil stop reason -> end_turn",
			stopReason: nil,
			expected:   "end_turn",
		},
		{
			name:       "stop -> end_turn",
			stopReason: strPtr("stop"),
			expected:   "end_turn",
		},
		{
			name:       "tool_use -> tool_use",
			stopReason: strPtr("tool_use"),
			expected:   "tool_use",
		},
		{
			name:       "max_tokens -> max_tokens",
			stopReason: strPtr("max_tokens"),
			expected:   "max_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &bifrostSchemas.BifrostResponsesResponse{
				Model:      "claude-3-sonnet",
				StopReason: tt.stopReason,
			}

			result, err := ConvertBifrostToAnthropic(resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.StopReason)
		})
	}
}

// --- Helper functions for tests ---

func ptr[T any](v T) *T {
	return &v
}

func strPtr(s string) *string {
	return &s
}
