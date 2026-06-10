package llmgateway

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConvertOpenAIStreamToAnthropic_TextStream(t *testing.T) {
	// Simulate OpenAI SSE stream with text content
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	rr := httptest.NewRecorder()
	err := ConvertOpenAIStreamToAnthropic(
		strings.NewReader(sseInput), rr, "gpt-4o",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := rr.Body.String()

	// Verify key Anthropic SSE events are present
	assertContains(t, output, "event: message_start")
	assertContains(t, output, "event: content_block_start")
	assertContains(t, output, "event: content_block_delta")
	assertContains(t, output, "event: content_block_stop")
	assertContains(t, output, "event: message_delta")
	assertContains(t, output, "event: message_stop")
	assertContains(t, output, `"text":"Hello"`)
	assertContains(t, output, `"text":" world"`)
	assertContains(t, output, `"stop_reason":"end_turn"`)
}

func TestConvertOpenAIStreamToAnthropic_ToolCalls(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-2","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"Tokyo\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-2","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	rr := httptest.NewRecorder()
	err := ConvertOpenAIStreamToAnthropic(
		strings.NewReader(sseInput), rr, "gpt-4o",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := rr.Body.String()

	assertContains(t, output, "event: message_start")
	assertContains(t, output, `"type":"tool_use"`)
	assertContains(t, output, `"name":"get_weather"`)
	assertContains(t, output, "event: content_block_delta")
	assertContains(t, output, `"input_json_delta"`)
	assertContains(t, output, `"stop_reason":"tool_use"`)
	assertContains(t, output, "event: message_stop")
}

func TestConvertOpenAIStreamToAnthropic_EmptyStream(t *testing.T) {
	sseInput := "data: [DONE]\n\n"

	rr := httptest.NewRecorder()
	err := ConvertOpenAIStreamToAnthropic(
		strings.NewReader(sseInput), rr, "gpt-4o",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := rr.Body.String()
	assertContains(t, output, "event: message_start")
	assertContains(t, output, "event: message_stop")
}

func TestConvertOpenAIStreamToAnthropic_EventOrdering(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-3","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-3","choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-3","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	rr := httptest.NewRecorder()
	err := ConvertOpenAIStreamToAnthropic(
		strings.NewReader(sseInput), rr, "gpt-4o",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify event ordering by scanning lines
	output := rr.Body.String()
	scanner := bufio.NewScanner(bytes.NewBufferString(output))
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
		}
	}

	expected := []string{
		"message_start",
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	}

	if len(events) != len(expected) {
		t.Fatalf("event count = %d, want %d; events: %v", len(events), len(expected), events)
	}
	for i, e := range expected {
		if events[i] != e {
			t.Errorf("event[%d] = %q, want %q", i, events[i], e)
		}
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("output does not contain %q", substr)
	}
}

// R3: Defense test - tool_calls present but finish_reason is "stop" instead of "tool_calls".
// The defense logic should force stop_reason to "tool_use".
func TestConvertOpenAIStreamToAnthropic_StopReasonDefense_ToolUse(t *testing.T) {
	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-d1","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-d1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_d1","type":"function","function":{"name":"Write","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-d1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"hello.py\"}"}}]},"finish_reason":null}]}`,
		``,
		// finish_reason is "stop" instead of "tool_calls" - this is the bug scenario
		`data: {"id":"chatcmpl-d1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	rr := httptest.NewRecorder()
	err := ConvertOpenAIStreamToAnthropic(
		strings.NewReader(sseInput), rr, "gpt-4o",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := rr.Body.String()

	// Even though finish_reason was "stop", the defense should override to "tool_use"
	// because tool blocks were detected during the stream.
	assertContains(t, output, `"stop_reason":"tool_use"`)
	assertContains(t, output, `"type":"tool_use"`)
	assertContains(t, output, `"name":"Write"`)
}

// TestConvertOpenAIStreamToAnthropic_LargeEvent verifies that SSE events
// exceeding the default bufio.Scanner buffer (64KB) are handled correctly.
func TestConvertOpenAIStreamToAnthropic_LargeEvent(t *testing.T) {
	// Generate a 100KB content string (well above 64KB default scanner limit).
	largeContent := strings.Repeat("x", 100*1024)

	sseInput := strings.Join([]string{
		`data: {"id":"lg","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		fmt.Sprintf(`data: {"id":"lg","choices":[{"delta":{"content":"%s"},"finish_reason":null}]}`, largeContent),
		``,
		`data: {"id":"lg","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	rr := httptest.NewRecorder()
	err := ConvertOpenAIStreamToAnthropic(strings.NewReader(sseInput), rr, "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := rr.Body.String()

	// Verify essential events are present.
	assertContains(t, output, `"type":"message_start"`)
	assertContains(t, output, `"type":"content_block_delta"`)
	assertContains(t, output, `"type":"message_stop"`)

	// Verify the large content was passed through.
	if !strings.Contains(output, largeContent) {
		t.Errorf("output does not contain the 100KB content")
	}
}
