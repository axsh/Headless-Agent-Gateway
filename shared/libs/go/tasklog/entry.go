package tasklog

import "time"

// Entry is a marker interface for task log entries.
type Entry interface {
	Timestamp() time.Time
	Type() string
}

// BaseEntry provides a common base for log entries.
type BaseEntry struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	EntryType string    `json:"entryType"`
}

func (b BaseEntry) Timestamp() time.Time {
	return b.Time
}

func (b BaseEntry) Type() string {
	return b.EntryType
}
