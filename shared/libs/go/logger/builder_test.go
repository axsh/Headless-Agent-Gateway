package logger

import (
	"strings"
	"testing"
)

func TestBuildFromConfig_StdoutOnly(t *testing.T) {
	outputs := []LogOutputConfig{
		{Type: "stdout"},
	}
	l, err := BuildFromConfig(LevelDebug, outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l == nil {
		t.Fatal("expected logger to be non-nil")
	}
	if l.level != LevelDebug {
		t.Errorf("expected level to be LevelDebug, got %v", l.level)
	}

	// Verify standard stdout writer is used
	if _, ok := l.writer.(*StdoutWriter); !ok {
		t.Errorf("expected writer to be *StdoutWriter, got %T", l.writer)
	}
}

func TestBuildFromConfig_SyslogOnly(t *testing.T) {
	outputs := []LogOutputConfig{
		{
			Type:    "syslog",
			Network: "udp",
			Address: "127.0.0.1:514",
			Tag:     "test-tag",
		},
	}
	l, err := BuildFromConfig(LevelInfo, outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l == nil {
		t.Fatal("expected logger to be non-nil")
	}

	sw, ok := l.writer.(*SyslogWriter)
	if !ok {
		t.Fatalf("expected writer to be *SyslogWriter, got %T", l.writer)
	}
	if sw.network != "udp" || sw.raddr != "127.0.0.1:514" || sw.tag != "test-tag" {
		t.Errorf("syslog writer fields mismatch: network=%q, raddr=%q, tag=%q", sw.network, sw.raddr, sw.tag)
	}
	if sw.stdoutInUse {
		t.Error("expected stdoutInUse to be false")
	}
}

func TestBuildFromConfig_Multiple(t *testing.T) {
	outputs := []LogOutputConfig{
		{Type: "stdout"},
		{
			Type:    "syslog",
			Network: "tcp",
			Address: "127.0.0.1:514",
			Tag:     "test-multiple",
		},
	}
	l, err := BuildFromConfig(LevelInfo, outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l == nil {
		t.Fatal("expected logger to be non-nil")
	}

	mw, ok := l.writer.(*MultiWriter)
	if !ok {
		t.Fatalf("expected writer to be *MultiWriter, got %T", l.writer)
	}

	if len(mw.writers) != 2 {
		t.Fatalf("expected 2 writers, got %d", len(mw.writers))
	}

	if _, ok := mw.writers[0].(*StdoutWriter); !ok {
		t.Errorf("expected first writer to be *StdoutWriter, got %T", mw.writers[0])
	}

	sw, ok := mw.writers[1].(*SyslogWriter)
	if !ok {
		t.Errorf("expected second writer to be *SyslogWriter, got %T", mw.writers[1])
	} else if !sw.stdoutInUse {
		t.Error("expected stdoutInUse to be true on syslog writer when stdout is also configured")
	}
}

func TestBuildFromConfig_Empty_DefaultsToStdout(t *testing.T) {
	l, err := BuildFromConfig(LevelWarn, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l == nil {
		t.Fatal("expected logger to be non-nil")
	}
	if _, ok := l.writer.(*StdoutWriter); !ok {
		t.Errorf("expected writer to be *StdoutWriter, got %T", l.writer)
	}
}

func TestBuildFromConfig_UnknownType(t *testing.T) {
	outputs := []LogOutputConfig{
		{Type: "file"},
	}
	_, err := BuildFromConfig(LevelInfo, outputs)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown log output type") {
		t.Errorf("expected 'unknown log output type' error, got %v", err)
	}
}
