package logger

import "os"

// StdoutWriter writes log output to os.Stdout (info/debug) and os.Stderr (warn/error).
type StdoutWriter struct{}

// NewStdoutWriter creates a new StdoutWriter.
func NewStdoutWriter() *StdoutWriter { return &StdoutWriter{} }

// Write writes the payload to stdout or stderr depending on the log level.
func (w *StdoutWriter) Write(level Level, payload []byte) (int, error) {
	if level >= LevelWarn {
		return os.Stderr.Write(payload)
	}
	return os.Stdout.Write(payload)
}

// Close is a no-op for StdoutWriter.
func (w *StdoutWriter) Close() error { return nil }
