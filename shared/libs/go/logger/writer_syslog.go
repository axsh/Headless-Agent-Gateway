package logger

import (
	"fmt"
	"net"
	"sync"
)

type SyslogWriter struct {
	mu      sync.Mutex
	network string
	raddr   string
	tag     string
	conn    net.Conn
}

func NewSyslogWriter(network, raddr, tag string) (*SyslogWriter, error) {
	w := &SyslogWriter{
		network: network,
		raddr:   raddr,
		tag:     tag,
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

func (w *SyslogWriter) Write(level Level, payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		if err := w.connect(); err != nil {
			return 0, err
		}
	}

	// Calculate syslog priority: (facility << 3) | severity
	// Facility: user (1)
	facility := 1
	var severity int
	switch level {
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
		// Connection failed, clear it to trigger reconnect next time
		_ = w.conn.Close()
		w.conn = nil
		return 0, err
	}

	return n, nil
}

func (w *SyslogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		err := w.conn.Close()
		w.conn = nil
		return err
	}
	return nil
}
