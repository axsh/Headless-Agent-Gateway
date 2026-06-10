package logger

import "strings"

// Level represents log severity.
type Level int

const (
	// LevelTrace is the most verbose log level (data dumps, request/response bodies).
	LevelTrace Level = iota
	// LevelDebug logs processing flow (branch decisions, lifecycle events).
	LevelDebug
	// LevelInfo is the default log level.
	LevelInfo
	// LevelWarn indicates potentially harmful situations.
	LevelWarn
	// LevelError indicates error events.
	LevelError
)

// String returns the string representation of the level.
func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a string into a Level.
// Returns LevelInfo if the string is not recognized.
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}
