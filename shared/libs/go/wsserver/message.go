package wsserver

import (
	"encoding/json"

	"github.com/axsh/hag/tasklog"
)

// Message is the WebSocket message envelope.
// The Type field determines how Payload should be decoded.
type Message struct {
	Type    string          `json:"type"`    // "log", "snapshot"
	Payload json.RawMessage `json:"payload"` // type-specific payload
}

// LogPayload carries a single log entry.
type LogPayload struct {
	Entry *tasklog.AgentLogEntry `json:"entry"`
}

// SnapshotPayload carries the full log history for new clients.
type SnapshotPayload struct {
	Entries []tasklog.Entry `json:"entries"`
}

// NewLogMessage creates a "log" type message from an AgentLogEntry.
func NewLogMessage(entry *tasklog.AgentLogEntry) ([]byte, error) {
	payload, err := json.Marshal(LogPayload{Entry: entry})
	if err != nil {
		return nil, err
	}
	msg := Message{Type: "log", Payload: payload}
	return json.Marshal(msg)
}

// NewSnapshotMessage creates a "snapshot" type message from log history.
func NewSnapshotMessage(entries []tasklog.Entry) ([]byte, error) {
	payload, err := json.Marshal(SnapshotPayload{Entries: entries})
	if err != nil {
		return nil, err
	}
	msg := Message{Type: "snapshot", Payload: payload}
	return json.Marshal(msg)
}
