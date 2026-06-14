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
	Seq        int              `json:"seq"`
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Timestamp  time.Time        `json:"timestamp"`
	ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// AppendHistory writes new messages to history/ as individual JSON files.
// Files are named with 9-digit zero-padded sequence numbers (e.g. 000000001.json).
// Existing files are never modified (append-only).
// startSeq is the last known sequence number; new messages start at startSeq+1.
func AppendHistory(histDir string, msgs []Message, startSeq int) error {
	for i, msg := range msgs {
		seq := startSeq + i + 1
		entry := HistoryEntry{
			Seq:        seq,
			Role:       msg.Role,
			Content:    msg.Content,
			Timestamp:  msg.Timestamp,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
		data, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal history entry %d: %w", seq, err)
		}
		filename := fmt.Sprintf("%09d.json", seq)
		if err := atomicWrite(filepath.Join(histDir, filename), data); err != nil {
			return fmt.Errorf("write history entry %d: %w", seq, err)
		}
	}
	return nil
}

// LoadHistory reads messages from history/ within [fromSeq, toSeq] range (inclusive).
// Missing sequence files are silently skipped (gap-tolerant).
func LoadHistory(histDir string, fromSeq, toSeq int) ([]Message, error) {
	var msgs []Message
	for seq := fromSeq; seq <= toSeq; seq++ {
		filename := fmt.Sprintf("%09d.json", seq)
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
		Role:       entry.Role,
		Content:    entry.Content,
		Timestamp:  entry.Timestamp,
		ToolCalls:  entry.ToolCalls,
		ToolCallID: entry.ToolCallID,
	}
}
