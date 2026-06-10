package llmgateway

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestConvertResponsesStreamToAnthropic_LargeEvent verifies that SSE events
// exceeding the default bufio.Scanner buffer (64KB) are handled correctly
// by the Responses API stream converter.
func TestConvertResponsesStreamToAnthropic_LargeEvent(t *testing.T) {
	// Generate a 100KB delta text (well above 64KB default scanner limit).
	largeText := strings.Repeat("y", 100*1024)

	sseInput := strings.Join([]string{
		`event: response.created`,
		`data: {"type":"response.created","response":{"id":"resp-lg"}}`,
		``,
		`event: response.content_part.added`,
		`data: {"type":"response.content_part.added"}`,
		``,
		`event: response.output_text.delta`,
		fmt.Sprintf(`data: {"type":"response.output_text.delta","delta":"%s"}`, largeText),
		``,
		`event: response.output_text.done`,
		`data: {"type":"response.output_text.done"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")

	var buf bytes.Buffer
	err := ConvertResponsesStreamToAnthropic(strings.NewReader(sseInput), &buf, "gpt-5.3-codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify essential Anthropic SSE events are present.
	if !strings.Contains(output, `"type":"message_start"`) {
		t.Error("output does not contain message_start")
	}
	if !strings.Contains(output, `"type":"content_block_delta"`) {
		t.Error("output does not contain content_block_delta")
	}
	if !strings.Contains(output, `"type":"content_block_stop"`) {
		t.Error("output does not contain content_block_stop")
	}
	if !strings.Contains(output, `"type":"message_delta"`) {
		t.Error("output does not contain message_delta")
	}
	if !strings.Contains(output, `"type":"message_stop"`) {
		t.Error("output does not contain message_stop")
	}

	// Verify the large content was passed through in the delta.
	if !strings.Contains(output, largeText) {
		t.Error("output does not contain the 100KB delta text")
	}
}
