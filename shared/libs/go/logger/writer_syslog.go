package logger

import (
	"fmt"
	"net"
	"os"
	"sync"
)

type stderrWriter struct{}

func (w *stderrWriter) Write(level Level, payload []byte) (int, error) {
	return os.Stderr.Write(payload)
}

func (w *stderrWriter) Close() error { return nil }

type SyslogWriter struct {
	mu          sync.Mutex
	network     string
	raddr       string
	tag         string
	conn        net.Conn
	fallback    LogWriter // fallback writer (stderr by default)
	hasFallback bool      // true when using fallback due to conn failure
	stdoutInUse bool      // true when stdout is also configured (skip fallback)
}

// NewSyslogWriter creates a SyslogWriter with fallback support.
// If stdoutInUse is true, syslog failure does NOT fallback to stderr
// (to avoid duplicate output).
func NewSyslogWriter(network, raddr, tag string, stdoutInUse bool) (*SyslogWriter, error) {
	w := &SyslogWriter{
		network:     network,
		raddr:       raddr,
		tag:         tag,
		fallback:    &stderrWriter{},
		stdoutInUse: stdoutInUse,
	}
	// Try initial connection but don't fail if server is down (syslog resilience rule)
	_ = w.connect()
	return w, nil
}

func (w *SyslogWriter) connect() error {
	conn, err := net.Dial(w.network, w.raddr)
	if err != nil {
		return err
	}
	w.conn = conn
	return nil
}

func (w *SyslogWriter) writeToSyslog(level Level, payload []byte) (int, error) {
	if w.conn == nil {
		return 0, fmt.Errorf("connection is nil")
	}

	// Calculate syslog priority: (facility << 3) | severity
	// Facility: user (1)
	facility := 1
	var severity int
	switch level {
	case LevelTrace:
		severity = 7 // debug (syslog doesn't have trace)
	case LevelDebug:
		severity = 7 // debug
	case LevelInfo:
		severity = 6 // info
	case LevelWarn:
		severity = 4 // warning
	case LevelError:
		severity = 3 // err
	default:
		severity = 6
	}

	pri := (facility << 3) | severity
	msg := fmt.Sprintf("<%d>%s: %s", pri, w.tag, string(payload))

	// Syslog packet often expects a trailing newline
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg += "\n"
	}

	n, err := w.conn.Write([]byte(msg))
	if err != nil {
		_ = w.conn.Close()
		w.conn = nil
		return 0, err
	}

	return n, nil
}

func (w *SyslogWriter) Write(level Level, payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		if err := w.connect(); err != nil {
			if !w.hasFallback {
				w.hasFallback = true
				warnMsg := fmt.Sprintf("WARN syslog connection failed, falling back to stderr: %v\n", err)
				_, _ = w.fallback.Write(LevelWarn, []byte(warnMsg))
			}
			if !w.stdoutInUse {
				return w.fallback.Write(level, payload)
			}
			return 0, nil
		}
	}

	if w.hasFallback {
		w.hasFallback = false
		// Log recovery message to syslog
		_, _ = w.writeToSyslog(LevelInfo, []byte("syslog connection recovered"))
	}

	n, err := w.writeToSyslog(level, payload)
	if err != nil {
		// Connection failed during write. Attempt fallback immediately.
		if !w.hasFallback {
			w.hasFallback = true
			warnMsg := fmt.Sprintf("WARN syslog connection failed during write, falling back to stderr: %v\n", err)
			_, _ = w.fallback.Write(LevelWarn, []byte(warnMsg))
		}
		if !w.stdoutInUse {
			return w.fallback.Write(level, payload)
		}
		return 0, nil
	}

	return n, nil
}

func (w *SyslogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var firstErr error
	if w.conn != nil {
		firstErr = w.conn.Close()
		w.conn = nil
	}
	if w.fallback != nil {
		if err := w.fallback.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
