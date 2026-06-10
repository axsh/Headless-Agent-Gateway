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

	writer, err := NewSyslogWriter("udp", localAddr, "test-tag", false)
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

func TestSyslogWriter_FallbackOnConnectFailure(t *testing.T) {
	writer, err := NewSyslogWriter("tcp", "127.0.0.1:9999", "test-fallback", false)
	if err != nil {
		t.Fatalf("unexpected error creating SyslogWriter: %v", err)
	}
	defer writer.Close()

	fb := newBufferWriter()
	writer.fallback = fb

	msg := []byte("hello fallback")
	_, err = writer.Write(LevelInfo, msg)
	if err != nil {
		t.Fatalf("unexpected error during write with fallback: %v", err)
	}

	out := fb.String()
	if !strings.Contains(out, "WARN syslog connection failed") {
		t.Errorf("expected warning in fallback output, got: %q", out)
	}
	if !strings.Contains(out, "hello fallback") {
		t.Errorf("expected payload in fallback output, got: %q", out)
	}

	// Verify that if stdoutInUse = true, the log payload itself is skipped, but warning is still written
	fb.Reset()
	writer.stdoutInUse = true
	writer.hasFallback = false // reset to allow warning to print again

	_, err = writer.Write(LevelInfo, msg)
	if err != nil {
		t.Fatalf("unexpected error during write: %v", err)
	}
	out = fb.String()
	if !strings.Contains(out, "WARN syslog connection failed") {
		t.Errorf("expected warning in fallback output, got: %q", out)
	}
	if strings.Contains(out, "hello fallback") {
		t.Errorf("expected payload to be skipped in fallback output when stdoutInUse is true, got: %q", out)
	}
}

func TestSyslogWriter_ReconnectOnRecovery(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	localAddr := l.Addr().String()

	// Accept connection in goroutine
	go func() {
		conn, err := l.Accept()
		if err == nil {
			buf := make([]byte, 1024)
			_, _ = conn.Read(buf)
			_ = conn.Close()
		}
	}()

	writer, err := NewSyslogWriter("tcp", localAddr, "test-reconnect", false)
	if err != nil {
		t.Fatalf("NewSyslogWriter failed: %v", err)
	}
	defer writer.Close()

	fb := newBufferWriter()
	writer.fallback = fb

	// Write first message - should connect and succeed
	_, err = writer.Write(LevelInfo, []byte("msg one"))
	if err != nil {
		t.Fatalf("Write 1 failed: %v", err)
	}

	// Now stop TCP listener to simulate failure
	_ = l.Close()
	// Force close writer connection to trigger reconnect
	if writer.conn != nil {
		_ = writer.conn.Close()
		writer.conn = nil
	}

	// Write second message - should fail to connect and fallback
	_, err = writer.Write(LevelInfo, []byte("msg two"))
	if err != nil {
		t.Fatalf("Write 2 failed: %v", err)
	}
	if !strings.Contains(fb.String(), "msg two") {
		t.Errorf("expected msg two in fallback, got: %q", fb.String())
	}

	// Now start TCP server again on the same port
	l2, err := net.Listen("tcp", localAddr)
	if err != nil {
		t.Fatalf("Listen 2 failed: %v", err)
	}
	defer l2.Close()

	var received string
	ch := make(chan struct{})
	go func() {
		conn, err := l2.Accept()
		if err == nil {
			buf := make([]byte, 4096)
			for {
				n, err := conn.Read(buf)
				if err != nil || n == 0 {
					break
				}
				received += string(buf[:n])
			}
			_ = conn.Close()
		}
		close(ch)
	}()

	// Write third message - should reconnect and write to TCP server, also logging recovery message
	_, err = writer.Write(LevelInfo, []byte("msg three"))
	if err != nil {
		t.Fatalf("Write 3 failed: %v", err)
	}

	// Close the writer to signal EOF to the TCP reader
	_ = writer.Close()

	<-ch
	if !strings.Contains(received, "syslog connection recovered") {
		t.Errorf("expected recovery message in syslog, got: %q", received)
	}
	if !strings.Contains(received, "msg three") {
		t.Errorf("expected msg three in syslog, got: %q", received)
	}
}
