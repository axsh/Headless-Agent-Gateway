package logger

import "time"

// Entry represents a single log entry used by DefaultLogger.
type Entry struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     Level          `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// NewEntry creates a new Entry with the current timestamp.
func NewEntry(level Level, msg string, fields map[string]any) *Entry {
	return &Entry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   msg,
		Fields:    fields,
	}
}
