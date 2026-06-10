package llmgateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// --- Request Conversion Tests ---

func TestConvertAnthropicRequestToResponses_BasicText(t *testing.T) {
	input := `{
		"model": "codex-mini-latest",
		"messages": [{"role": "user", "content": "hello world"}],
		"max_tokens": 1024
	}`

	result, err := ConvertAnthropicRequestToResponses([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req ResponsesRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if req.Model != "codex-mini-latest" {
		t.Errorf("Model = %q, want %q", req.Model, "codex-mini-latest")
	}
	if len(req.Input) != 1 {
		t.Fatalf("Input length = %d, want 1", len(req.Input))
	}
	if req.Input[0].Role != "user" {
		t.Errorf("Input[0].Role = %q, want %q", req.Input[0].Role, "user")
	}
	if req.Input[0].Content != "hello world" {
		t.Errorf("Input[0].Content = %q, want %q", req.Input[0].Content, "hello world")
	}
}

func TestConvertAnthropicRequestToResponses_WithSystem(t *testing.T) {
	input := `{
		"model": "codex-mini-latest",
		"system": "You are a helpful assistant",
		"messages": [{"role": "user", "content": "hi"}],
		"max_tokens": 1024
	}`

	result, err := ConvertAnthropicRequestToResponses([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req ResponsesRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// System message should be prepended as "developer" role.
	if len(req.Input) < 2 {
		t.Fatalf("Input length = %d, want >= 2", len(req.Input))
	}
	if req.Input[0].Role != "developer" {
		t.Errorf("Input[0].Role = %q, want %q", req.Input[0].Role, "developer")
	}
	if req.Input[0].Content != "You are a helpful assistant" {
		t.Errorf("Input[0].Content = %q, want system text", req.Input[0].Content)
	}
	if req.Input[1].Role != "user" {
		t.Errorf("Input[1].Role = %q, want %q", req.Input[1].Role, "user")
	}
}

func TestConvertAnthropicRequestToResponses_WithTools(t *testing.T) {
	input := `{
		"model": "codex-mini-latest",
		"messages": [{"role": "user", "content": "what is the weather?"}],
		"max_tokens": 1024,
		"tools": [{
			"name": "get_weather",
			"description": "Get weather info",
			"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}
		}]
	}`

	result, err := ConvertAnthropicRequestToResponses([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req ResponsesRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(req.Tools) != 1 {
		t.Fatalf("Tools length = %d, want 1", len(req.Tools))
	}
	if req.Tools[0].Type != "function" {
		t.Errorf("Tools[0].Type = %q, want %q", req.Tools[0].Type, "function")
	}
	if req.Tools[0].Name != "get_weather" {
		t.Errorf("Tools[0].Name = %q, want %q", req.Tools[0].Name, "get_weather")
	}
	if req.Tools[0].Description != "Get weather info" {
		t.Errorf("Tools[0].Description = %q, want %q", req.Tools[0].Description, "Get weather info")
	}
	if len(req.Tools[0].Parameters) == 0 {
		t.Error("Tools[0].Parameters should not be empty")
	}
}

func TestConvertAnthropicRequestToResponses_MaxTokensClamp(t *testing.T) {
	input := `{
		"model": "codex-mini-latest",
		"messages": [{"role": "user", "content": "hi"}],
		"max_tokens": 32000
	}`

	result, err := ConvertAnthropicRequestToResponses([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req ResponsesRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if req.MaxOutputTokens == nil {
		t.Fatal("MaxOutputTokens should not be nil")
	}
	if *req.MaxOutputTokens != openAIMaxCompletionTokens {
		t.Errorf("MaxOutputTokens = %d, want %d", *req.MaxOutputTokens, openAIMaxCompletionTokens)
	}
}

func TestConvertAnthropicRequestToResponses_Stream(t *testing.T) {
	input := `{
		"model": "codex-mini-latest",
		"messages": [{"role": "user", "content": "hi"}],
		"max_tokens": 1024,
		"stream": true
	}`

	result, err := ConvertAnthropicRequestToResponses([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req ResponsesRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if req.Stream == nil || !*req.Stream {
		t.Error("Stream should be true")
	}
}

func TestConvertAnthropicRequestToResponses_ToolResultMessage(t *testing.T) {
	input := `{
		"model": "codex-mini-latest",
		"messages": [
			{"role": "user", "content": "get weather"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "call_1", "name": "get_weather", "input": {"city": "Tokyo"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "call_1", "content": "sunny, 25C"}
			]}
		],
		"max_tokens": 1024
	}`

	result, err := ConvertAnthropicRequestToResponses([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req ResponsesRequest
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// Should contain: user, assistant, function_call, function_call_output
	found := false
	for _, inp := range req.Input {
		if inp.Type == "function_call_output" {
			found = true
			if inp.CallID != "call_1" {
				t.Errorf("function_call_output CallID = %q, want %q", inp.CallID, "call_1")
			}
			if inp.Output != "sunny, 25C" {
				t.Errorf("function_call_output Output = %q, want %q", inp.Output, "sunny, 25C")
			}
		}
	}
	if !found {
		t.Error("expected function_call_output in input, but not found")
	}
}

// --- Response Conversion Tests ---

func TestConvertResponsesResponseToAnthropic_TextOnly(t *testing.T) {
	respBody := `{
		"id": "resp_abc123",
		"status": "completed",
		"output": [{
			"type": "message",
			"content": [{"type": "output_text", "text": "Hello from Codex!"}]
		}],
		"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`

	result, err := ConvertResponsesResponseToAnthropic([]byte(respBody), "codex-mini-latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if resp.ID != "resp_abc123" {
		t.Errorf("ID = %q, want %q", resp.ID, "resp_abc123")
	}
	if resp.Type != "message" {
		t.Errorf("Type = %q, want %q", resp.Type, "message")
	}
	if resp.Role != "assistant" {
		t.Errorf("Role = %q, want %q", resp.Role, "assistant")
	}
	if resp.Model != "codex-mini-latest" {
		t.Errorf("Model = %q, want %q", resp.Model, "codex-mini-latest")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Content[0].Type = %q, want %q", resp.Content[0].Type, "text")
	}
	if resp.Content[0].Text != "Hello from Codex!" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello from Codex!")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("Usage.OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
}

func TestConvertResponsesResponseToAnthropic_WithToolCalls(t *testing.T) {
	respBody := `{
		"id": "resp_tool123",
		"status": "completed",
		"output": [
			{"type": "function_call", "call_id": "call_abc", "name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}
		],
		"usage": {"input_tokens": 20, "output_tokens": 10, "total_tokens": 30}
	}`

	result, err := ConvertResponsesResponseToAnthropic([]byte(respBody), "codex-mini-latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "tool_use")
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(resp.Content))
	}
	if resp.Content[0].Type != "tool_use" {
		t.Errorf("Content[0].Type = %q, want %q", resp.Content[0].Type, "tool_use")
	}
	if resp.Content[0].ID != "call_abc" {
		t.Errorf("Content[0].ID = %q, want %q", resp.Content[0].ID, "call_abc")
	}
	if resp.Content[0].Name != "get_weather" {
		t.Errorf("Content[0].Name = %q, want %q", resp.Content[0].Name, "get_weather")
	}
}

func TestConvertResponsesResponseToAnthropic_EmptyOutput(t *testing.T) {
	respBody := `{
		"id": "resp_empty",
		"status": "completed",
		"output": [],
		"usage": {"input_tokens": 5, "output_tokens": 0, "total_tokens": 5}
	}`

	result, err := ConvertResponsesResponseToAnthropic([]byte(respBody), "codex-mini-latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if len(resp.Content) != 0 {
		t.Errorf("Content length = %d, want 0", len(resp.Content))
	}
}

// --- Streaming Conversion Tests ---

func TestConvertResponsesStreamToAnthropic_TextStream(t *testing.T) {
	// Simulate Responses API SSE events for a text-only response.
	events := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_s1","status":"in_progress"}}`,
		"",
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","content":[]}}`,
		"",
		"event: response.content_part.added",
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hello "}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"world!"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_s1","status":"completed"}}`,
		"",
		"",
	}, "\n")

	reader := strings.NewReader(events)
	var buf bytes.Buffer
	err := ConvertResponsesStreamToAnthropic(reader, &buf, "codex-mini-latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify message_start event
	if !strings.Contains(output, `"type":"message_start"`) {
		t.Error("expected message_start event in output")
	}

	// Verify text delta events
	if !strings.Contains(output, `"type":"text_delta"`) {
		t.Error("expected text_delta event in output")
	}
	if !strings.Contains(output, `"text":"Hello "`) {
		t.Error("expected 'Hello ' delta in output")
	}
	if !strings.Contains(output, `"text":"world!"`) {
		t.Error("expected 'world!' delta in output")
	}

	// Verify message_stop event
	if !strings.Contains(output, `"type":"message_stop"`) {
		t.Error("expected message_stop event in output")
	}
}

func TestConvertResponsesStreamToAnthropic_ToolCallStream(t *testing.T) {
	events := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_tc","status":"in_progress"}}`,
		"",
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_tc1","name":"get_weather"}}`,
		"",
		"event: response.function_call_arguments.delta",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"city\":"}`,
		"",
		"event: response.function_call_arguments.delta",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"Tokyo\"}"}`,
		"",
		"event: response.function_call_arguments.done",
		`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"city\":\"Tokyo\"}"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_tc","status":"completed"}}`,
		"",
		"",
	}, "\n")

	reader := strings.NewReader(events)
	var buf bytes.Buffer
	err := ConvertResponsesStreamToAnthropic(reader, &buf, "codex-mini-latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify content_block_start with tool_use type
	if !strings.Contains(output, `"type":"tool_use"`) {
		t.Error("expected tool_use content_block_start in output")
	}
	if !strings.Contains(output, `"name":"get_weather"`) {
		t.Error("expected tool name 'get_weather' in output")
	}

	// Verify input_json_delta events
	if !strings.Contains(output, `"type":"input_json_delta"`) {
		t.Error("expected input_json_delta event in output")
	}
}

func TestConvertResponsesStreamToAnthropic_EventOrdering(t *testing.T) {
	events := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_ord","status":"in_progress"}}`,
		"",
		"event: response.output_item.added",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","content":[]}}`,
		"",
		"event: response.content_part.added",
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"Hi"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_ord","status":"completed"}}`,
		"",
		"",
	}, "\n")

	reader := strings.NewReader(events)
	var buf bytes.Buffer
	err := ConvertResponsesStreamToAnthropic(reader, &buf, "codex-mini-latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify ordering: message_start before text_delta before message_stop
	startIdx := strings.Index(output, "message_start")
	deltaIdx := strings.Index(output, "text_delta")
	stopIdx := strings.Index(output, "message_stop")

	if startIdx < 0 || deltaIdx < 0 || stopIdx < 0 {
		t.Fatalf("missing events: start=%d, delta=%d, stop=%d", startIdx, deltaIdx, stopIdx)
	}
	if startIdx >= deltaIdx {
		t.Errorf("message_start (pos %d) should come before text_delta (pos %d)", startIdx, deltaIdx)
	}
	if deltaIdx >= stopIdx {
		t.Errorf("text_delta (pos %d) should come before message_stop (pos %d)", deltaIdx, stopIdx)
	}
}
