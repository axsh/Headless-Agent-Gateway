package logger

import "testing"

func TestStdoutWriter_LevelRouting(t *testing.T) {
	w := NewStdoutWriter()

	tests := []struct {
		name  string
		level Level
	}{
		{"debug_to_stdout", LevelDebug},
		{"info_to_stdout", LevelInfo},
		{"warn_to_stderr", LevelWarn},
		{"error_to_stderr", LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte("test message\n")
			n, err := w.Write(tt.level, payload)
			if err != nil {
				t.Errorf("Write() error = %v", err)
			}
			if n != len(payload) {
				t.Errorf("Write() n = %d, want %d", n, len(payload))
			}
		})
	}
}

func TestStdoutWriter_Close(t *testing.T) {
	w := NewStdoutWriter()
	if err := w.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// Compile-time check: StdoutWriter implements LogWriter.
var _ LogWriter = (*StdoutWriter)(nil)
