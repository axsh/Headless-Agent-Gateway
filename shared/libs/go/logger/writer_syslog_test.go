package logger

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestSyslogWriter_Write(t *testing.T) {
	// Setup dummy UDP server
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ResolveUDPAddr failed: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("ListenUDP failed: %v", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().String()

	writer, err := NewSyslogWriter("udp", localAddr, "test-tag")
	if err != nil {
		t.Fatalf("NewSyslogWriter failed: %v", err)
	}
	defer writer.Close()

	msg := []byte("hello syslog")
	n, err := writer.Write(LevelInfo, msg)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n == 0 {
		t.Errorf("expected non-zero bytes written")
	}

	buf := make([]byte, 1024)
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	nRead, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}

	received := string(buf[:nRead])
	if !strings.Contains(received, "hello syslog") {
		t.Errorf("expected %q to contain %q", received, "hello syslog")
	}
}
