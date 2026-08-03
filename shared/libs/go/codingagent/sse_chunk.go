package codingagent

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

const (
	// DefaultMaxSSEDataLineBytes is the max JSON size for a single SSE data line.
	DefaultMaxSSEDataLineBytes = 64 * 1024 // 64 KiB
	// DefaultSSEChunkContentBytes is the max content bytes per tool_result_part chunk.
	DefaultSSEChunkContentBytes = 48 * 1024 // 48 KiB
)

// SplitStreamEventForSSE splits oversized EventToolResult into wire-safe events.
// Non-EventToolResult events are returned unchanged as a single-element slice.
// maxLineBytes <= 0 uses DefaultMaxSSEDataLineBytes.
func SplitStreamEventForSSE(ev StreamEvent, maxLineBytes int) ([]StreamEvent, error) {
	if ev.Type != EventToolResult {
		return []StreamEvent{ev}, nil
	}

	limit := maxLineBytes
	if limit <= 0 {
		limit = DefaultMaxSSEDataLineBytes
	}

	wire, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("marshal stream event: %w", err)
	}
	if len(wire) < limit {
		return []StreamEvent{ev}, nil
	}

	for chunkSize := DefaultSSEChunkContentBytes; chunkSize >= 4096; chunkSize /= 2 {
		out, ok, err := buildChunkedToolResult(ev, chunkSize, limit)
		if err != nil {
			return nil, err
		}
		if ok {
			return out, nil
		}
	}
	return nil, fmt.Errorf("cannot split tool_result under %d bytes wire limit", limit)
}

func buildChunkedToolResult(ev StreamEvent, chunkSize, limit int) ([]StreamEvent, bool, error) {
	chunkID := uuid.New().String()
	content := ev.Content
	total := (len(content) + chunkSize - 1) / chunkSize
	if total == 0 {
		total = 1
	}

	out := make([]StreamEvent, 0, total+1)
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(content) {
			end = len(content)
		}
		out = append(out, StreamEvent{
			Type:       EventToolResultPart,
			Content:    content[start:end],
			ToolName:   ev.ToolName,
			ChunkID:    chunkID,
			ChunkIndex: i,
			ChunkTotal: total,
		})
	}

	out = append(out, StreamEvent{
		Type:     EventToolResult,
		ToolName: ev.ToolName,
		ChunkID:  chunkID,
		Content:  "",
	})

	for _, wireEv := range out {
		data, err := json.Marshal(wireEv)
		if err != nil {
			return nil, false, fmt.Errorf("marshal stream event: %w", err)
		}
		if len(data) >= limit {
			return nil, false, nil
		}
	}
	return out, true, nil
}

// ReassembleToolResultParts joins tool_result_part content in order.
func ReassembleToolResultParts(events []StreamEvent) (string, error) {
	if len(events) == 0 {
		return "", fmt.Errorf("no chunk events")
	}
	chunkID := events[0].ChunkID
	total := events[0].ChunkTotal
	parts := make(map[int]string, len(events))
	for _, ev := range events {
		if ev.Type != EventToolResultPart {
			return "", fmt.Errorf("expected tool_result_part, got %q", ev.Type)
		}
		if ev.ChunkID != chunkID {
			return "", fmt.Errorf("chunk_id mismatch")
		}
		if ev.ChunkTotal != total {
			return "", fmt.Errorf("chunk total mismatch")
		}
		if _, ok := parts[ev.ChunkIndex]; ok {
			return "", fmt.Errorf("duplicate chunk index %d", ev.ChunkIndex)
		}
		parts[ev.ChunkIndex] = ev.Content
	}
	var assembled string
	for i := 0; i < total; i++ {
		part, ok := parts[i]
		if !ok {
			return "", fmt.Errorf("missing chunk index %d", i)
		}
		assembled += part
	}
	return assembled, nil
}
