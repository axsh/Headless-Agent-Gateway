package logger

import (
	"strings"
	"testing"
)

func TestDefaultLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name      string
		loggerLvl Level
		callLvl   string
		shouldLog bool
	}{
		{"info_blocks_debug", LevelInfo, "debug", false},
		{"info_passes_info", LevelInfo, "info", true},
		{"info_passes_warn", LevelInfo, "warn", true},
		{"info_passes_error", LevelInfo, "error", true},
		{"debug_passes_debug", LevelDebug, "debug", true},
		{"error_blocks_info", LevelError, "info", false},
		{"error_blocks_warn", LevelError, "warn", false},
		{"error_passes_error", LevelError, "error", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newBufferWriter()
			l := NewDefaultWithOptions(tt.loggerLvl, &TextFormatter{}, w)

			msg := "test message"
			switch tt.callLvl {
			case "debug":
				l.Debug(msg)
			case "info":
				l.Info(msg)
			case "warn":
				l.Warn(msg)
			case "error":
				l.Error(msg)
			}

			hasOutput := w.String() != ""
			if hasOutput != tt.shouldLog {
				t.Errorf("shouldLog=%v but hasOutput=%v, output=%q",
					tt.shouldLog, hasOutput, w.String())
			}
		})
	}
}

func TestDefaultLogger_StructuredFields_Text(t *testing.T) {
	w := newBufferWriter()
	l := NewDefaultWithOptions(LevelDebug, &TextFormatter{}, w)

	l.Info("hello", "user", "alice", "count", 5)

	s := w.String()
	if !strings.Contains(s, "INFO hello") {
		t.Errorf("missing level/message in %q", s)
	}
	if !strings.Contains(s, "count=5") {
		t.Errorf("missing count field in %q", s)
	}
	if !strings.Contains(s, "user=alice") {
		t.Errorf("missing user field in %q", s)
	}
}

func TestDefaultLogger_StructuredFields_JSON(t *testing.T) {
	w := newBufferWriter()
	l := NewDefaultWithOptions(LevelDebug, &JSONFormatter{}, w)

	l.Info("hello", "user", "alice")

	s := w.String()
	if !strings.Contains(s, `"message":"hello"`) {
		t.Errorf("missing message in %q", s)
	}
	if !strings.Contains(s, `"user":"alice"`) {
		t.Errorf("missing user field in %q", s)
	}
}

func TestDefaultLogger_OddLengthFields(t *testing.T) {
	w := newBufferWriter()
	l := NewDefaultWithOptions(LevelDebug, &TextFormatter{}, w)

	l.Info("msg", "key1", "val1", "orphan")

	s := w.String()
	if !strings.Contains(s, "key1=val1") {
		t.Errorf("missing key1 in %q", s)
	}
	if !strings.Contains(s, "orphan=MISSING_VALUE") {
		t.Errorf("missing orphan/MISSING_VALUE in %q", s)
	}
}

func TestDefaultLogger_WithComponent(t *testing.T) {
	w := newBufferWriter()
	l := NewDefaultWithOptions(LevelDebug, &TextFormatter{}, w)

	child := l.WithComponent("gateway")
	child.Info("started")

	s := w.String()
	if !strings.Contains(s, "component=gateway") {
		t.Errorf("missing component field in %q", s)
	}

	// Grandchild with additional fields
	w.Reset()
	grandchild := child.WithFields(map[string]any{"port": 14000})
	grandchild.Info("listen")

	s = w.String()
	if !strings.Contains(s, "component=gateway") {
		t.Errorf("missing component in grandchild output %q", s)
	}
	if !strings.Contains(s, "port=14000") {
		t.Errorf("missing port in grandchild output %q", s)
	}
}

func TestDefaultLogger_WithFields_Immutable(t *testing.T) {
	w := newBufferWriter()
	parent := NewDefaultWithOptions(LevelDebug, &TextFormatter{}, w)
	parent = parent.WithFields(map[string]any{"env": "prod"}).(*DefaultLogger)

	child := parent.WithFields(map[string]any{"req_id": "abc"})

	// Log from parent
	parent.Info("parent log")
	parentOutput := w.String()

	w.Reset()
	// Log from child
	child.Info("child log")
	childOutput := w.String()

	// Parent should have env but NOT req_id
	if !strings.Contains(parentOutput, "env=prod") {
		t.Errorf("parent missing env field: %q", parentOutput)
	}
	if strings.Contains(parentOutput, "req_id") {
		t.Errorf("parent should not have req_id: %q", parentOutput)
	}

	// Child should have both env and req_id
	if !strings.Contains(childOutput, "env=prod") {
		t.Errorf("child missing env field: %q", childOutput)
	}
	if !strings.Contains(childOutput, "req_id=abc") {
		t.Errorf("child missing req_id field: %q", childOutput)
	}
}
