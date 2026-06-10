package logger

import "maps"

// DefaultLogger implements Logger with pluggable Formatter and LogWriter.
// It follows vv4's strategy pattern for output and format customization.
type DefaultLogger struct {
	level     Level
	formatter Formatter
	writer    LogWriter
	fields    map[string]any
}

// NewDefault creates a DefaultLogger with TextFormatter and StdoutWriter at the given level.
func NewDefault(level Level) *DefaultLogger {
	return &DefaultLogger{
		level:     level,
		formatter: &TextFormatter{},
		writer:    NewStdoutWriter(),
		fields:    make(map[string]any),
	}
}

// NewDefaultWithOptions creates a DefaultLogger with custom formatter and writer.
func NewDefaultWithOptions(level Level, formatter Formatter, writer LogWriter) *DefaultLogger {
	return &DefaultLogger{
		level:     level,
		formatter: formatter,
		writer:    writer,
		fields:    make(map[string]any),
	}
}

// Trace logs a trace-level message.
func (l *DefaultLogger) Trace(msg string, fields ...any) {
	l.log(LevelTrace, msg, fields)
}

// Debug logs a debug-level message.
func (l *DefaultLogger) Debug(msg string, fields ...any) {
	l.log(LevelDebug, msg, fields)
}

// Info logs an info-level message.
func (l *DefaultLogger) Info(msg string, fields ...any) {
	l.log(LevelInfo, msg, fields)
}

// Warn logs a warning-level message.
func (l *DefaultLogger) Warn(msg string, fields ...any) {
	l.log(LevelWarn, msg, fields)
}

// Error logs an error-level message.
func (l *DefaultLogger) Error(msg string, fields ...any) {
	l.log(LevelError, msg, fields)
}

// WithFields returns a new DefaultLogger with merged fields.
// The original logger is not modified (immutable).
func (l *DefaultLogger) WithFields(fields map[string]any) Logger {
	merged := make(map[string]any, len(l.fields)+len(fields))
	maps.Copy(merged, l.fields)
	maps.Copy(merged, fields)
	return &DefaultLogger{
		level:     l.level,
		formatter: l.formatter,
		writer:    l.writer,
		fields:    merged,
	}
}

// WithComponent returns a new DefaultLogger with "component" field set.
func (l *DefaultLogger) WithComponent(name string) Logger {
	return l.WithFields(map[string]any{"component": name})
}

// log handles the core logging logic: level check -> entry creation -> format -> write.
func (l *DefaultLogger) log(level Level, msg string, kvPairs []any) {
	if level < l.level {
		return
	}

	// Merge base fields with inline key-value pairs.
	allFields := make(map[string]any, len(l.fields)+len(kvPairs)/2)
	maps.Copy(allFields, l.fields)

	// Parse alternating key-value pairs.
	for i := 0; i < len(kvPairs); i += 2 {
		key, ok := kvPairs[i].(string)
		if !ok {
			key = "INVALID_KEY"
		}
		var val any
		if i+1 < len(kvPairs) {
			val = kvPairs[i+1]
		} else {
			val = "MISSING_VALUE"
		}
		allFields[key] = val
	}

	entry := NewEntry(level, msg, allFields)

	data, err := l.formatter.Format(entry)
	if err != nil {
		// Best-effort: write error to writer directly.
		return
	}

	_, _ = l.writer.Write(level, data)
}
