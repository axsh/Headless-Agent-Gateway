package logger

// Logger defines the logging interface for tern components.
// In-Process users can inject their own implementation (slog, zap, syslog, etc.)
// via tern.WithLogger(). If not provided, DefaultLogger is used.
type Logger interface {
	// Trace logs trace-level data dumps (JSON bodies, headers, full payloads).
	Trace(msg string, fields ...any)

	// Debug logs a debug-level message with optional key-value fields.
	// fields are alternating key (string) and value (any) pairs.
	Debug(msg string, fields ...any)

	// Info logs an info-level message.
	Info(msg string, fields ...any)

	// Warn logs a warning-level message.
	Warn(msg string, fields ...any)

	// Error logs an error-level message.
	Error(msg string, fields ...any)

	// WithFields returns a child logger with additional fields.
	// The original logger is not modified (immutable).
	WithFields(fields map[string]any) Logger

	// WithComponent returns a child logger with "component" field set.
	// Called in each component's New() to tag subsequent logs.
	WithComponent(name string) Logger
}
