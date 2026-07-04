package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HistoryEntry represents a single conversation turn persisted in history/.
type HistoryEntry struct {
	Seq          int              `json:"seq"`
	Role         string           `json:"role"`
	Content      string           `json:"content"`
	ContentParts []ContentPart    `json:"content_parts,omitempty"`
	Timestamp    time.Time        `json:"timestamp"`
	ToolCalls    []ToolCallRecord `json:"tool_calls,omitempty"`
	ToolCallID   string           `json:"tool_call_id,omitempty"`
}

// AppendHistory writes new messages to histDir as individual JSON files.
// Files are named with 7-digit zero-padded hex sequence numbers (e.g. 0000001.json).
// The caller is responsible for constructing histDir with any subdirectory path
// (e.g. "history/000000a/" for child sessions).
// Existing files are never modified (append-only).
func AppendHistory(histDir string, msgs []Message) error {
	for _, msg := range msgs {
		filename := fmt.Sprintf("%07x.json", msg.Seq)
		entry := HistoryEntry{
			Seq:          msg.Seq,
			Role:         msg.Role,
			Content:      msg.Content,
			ContentParts: msg.ContentParts,
			Timestamp:    msg.Timestamp,
			ToolCalls:    msg.ToolCalls,
			ToolCallID:   msg.ToolCallID,
		}
		data, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal history entry %d: %w", msg.Seq, err)
		}
		targetPath := filepath.Join(histDir, filename)
		// Skip if file already exists (append-only, never overwrite).
		if _, err := os.Stat(targetPath); err == nil {
			continue
		}
		if err := atomicWrite(targetPath, data); err != nil {
			return fmt.Errorf("write history entry %d: %w", msg.Seq, err)
		}
	}
	return nil
}

// LoadHistory reads messages from history/ within [fromSeq, toSeq] range (inclusive).
// Missing sequence files are silently skipped (gap-tolerant).
func LoadHistory(histDir string, fromSeq, toSeq int) ([]Message, error) {
	var msgs []Message
	for seq := fromSeq; seq <= toSeq; seq++ {
		filename := fmt.Sprintf("%07x.json", seq)
		data, err := os.ReadFile(filepath.Join(histDir, filename))
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip missing files (gap-tolerant).
			}
			return nil, fmt.Errorf("read history entry %d: %w", seq, err)
		}
		var entry HistoryEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("unmarshal history entry %d: %w", seq, err)
		}
		msgs = append(msgs, entryToMessage(entry))
	}
	return msgs, nil
}

// entryToMessage converts a HistoryEntry to a session.Message.
func entryToMessage(entry HistoryEntry) Message {
	return Message{
		Role:         entry.Role,
		Content:      entry.Content,
		ContentParts: entry.ContentParts,
		Timestamp:    entry.Timestamp,
		Seq:          entry.Seq,
		ToolCalls:    entry.ToolCalls,
		ToolCallID:   entry.ToolCallID,
	}
}
