package logger

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTextFormatter_Format(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Level:     LevelInfo,
		Message:   "hello world",
		Fields:    map[string]any{"user": "alice", "count": 5},
	}

	f := &TextFormatter{}
	got, err := f.Format(entry)
	if err != nil {
		t.Fatalf("TextFormatter.Format() error = %v", err)
	}

	s := string(got)
	// Verify timestamp, level, message
	if !strings.Contains(s, "2026-01-01T00:00:00Z") {
		t.Errorf("missing timestamp in %q", s)
	}
	if !strings.Contains(s, "INFO") {
		t.Errorf("missing level in %q", s)
	}
	if !strings.Contains(s, "hello world") {
		t.Errorf("missing message in %q", s)
	}
	// Verify fields (sorted: count before user)
	if !strings.Contains(s, "count=5") {
		t.Errorf("missing count field in %q", s)
	}
	if !strings.Contains(s, "user=alice") {
		t.Errorf("missing user field in %q", s)
	}
	// Verify alphabetical order: count before user
	countIdx := strings.Index(s, "count=5")
	userIdx := strings.Index(s, "user=alice")
	if countIdx > userIdx {
		t.Errorf("fields not sorted alphabetically: count at %d, user at %d", countIdx, userIdx)
	}
	// Verify trailing newline
	if !strings.HasSuffix(s, "\n") {
		t.Errorf("missing trailing newline in %q", s)
	}
}

func TestTextFormatter_Format_NoFields(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Level:     LevelError,
		Message:   "something failed",
	}

	f := &TextFormatter{}
	got, err := f.Format(entry)
	if err != nil {
		t.Fatalf("TextFormatter.Format() error = %v", err)
	}

	s := string(got)
	want := "2026-01-01T00:00:00Z ERROR something failed\n"
	if s != want {
		t.Errorf("got %q, want %q", s, want)
	}
}

func TestJSONFormatter_Format(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Level:     LevelWarn,
		Message:   "warning msg",
		Fields:    map[string]any{"key": "value"},
	}

	f := &JSONFormatter{}
	got, err := f.Format(entry)
	if err != nil {
		t.Fatalf("JSONFormatter.Format() error = %v", err)
	}

	// Verify it is valid JSON (strip trailing newline)
	s := strings.TrimSuffix(string(got), "\n")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %q", err, s)
	}

	// Verify fields exist
	if parsed["message"] != "warning msg" {
		t.Errorf("message = %v, want %q", parsed["message"], "warning msg")
	}
	fields, ok := parsed["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields is not a map: %T", parsed["fields"])
	}
	if fields["key"] != "value" {
		t.Errorf("fields.key = %v, want %q", fields["key"], "value")
	}

	// Verify trailing newline
	if !strings.HasSuffix(string(got), "\n") {
		t.Errorf("missing trailing newline")
	}
}

func TestJSONFormatter_Format_NoFields(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Level:     LevelInfo,
		Message:   "hello",
	}

	f := &JSONFormatter{}
	got, err := f.Format(entry)
	if err != nil {
		t.Fatalf("JSONFormatter.Format() error = %v", err)
	}

	s := strings.TrimSuffix(string(got), "\n")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if parsed["message"] != "hello" {
		t.Errorf("message = %v, want %q", parsed["message"], "hello")
	}
}
