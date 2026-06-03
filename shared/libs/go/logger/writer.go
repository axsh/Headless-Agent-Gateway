package logger

// LogWriter writes formatted log output to a destination.
type LogWriter interface {
	// Write writes the payload. Level is provided for priority-aware writers (e.g. syslog).
	Write(level Level, payload []byte) (int, error)

	// Close releases any resources held by the writer.
	Close() error
}
