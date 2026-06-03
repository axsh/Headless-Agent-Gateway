package logger

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Formatter formats a log Entry into bytes.
type Formatter interface {
	Format(*Entry) ([]byte, error)
}

// TextFormatter formats logs as "TIMESTAMP LEVEL message key1=val1 key2=val2" lines.
// Fields are sorted alphabetically for deterministic output.
type TextFormatter struct{}

// Format implements Formatter.
func (f *TextFormatter) Format(entry *Entry) ([]byte, error) {
	timestamp := entry.Timestamp.Format(time.RFC3339)
	level := entry.Level.String()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s %s", timestamp, level, entry.Message))

	if len(entry.Fields) > 0 {
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(entry.Fields))
		for k := range entry.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			sb.WriteString(fmt.Sprintf(" %s=%v", k, entry.Fields[k]))
		}
	}
	sb.WriteByte('\n')

	return []byte(sb.String()), nil
}

// JSONFormatter formats logs as JSON objects, one per line.
type JSONFormatter struct{}

// Format implements Formatter.
func (f *JSONFormatter) Format(entry *Entry) ([]byte, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log entry: %w", err)
	}
	return append(data, '\n'), nil
}
