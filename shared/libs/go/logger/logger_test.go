package logger

import "testing"

// Compile-time checks: DefaultLogger implements Logger.
var _ Logger = (*DefaultLogger)(nil)

// Compile-time checks: mockLogger implements Logger.
var _ Logger = (*mockLogger)(nil)

func TestLogger_InterfaceCompliance(t *testing.T) {
	// DefaultLogger must satisfy Logger interface.
	var l Logger = NewDefault(LevelInfo)
	l.Info("interface check")
}

func TestCustomLogger_Injection(t *testing.T) {
	m := newMockLogger()
	// All Logger methods must be callable.
	m.Debug("d", "k", "v")
	m.Info("i")
	m.Warn("w")
	m.Error("e")
	child := m.WithFields(map[string]any{"a": 1})
	child2 := m.WithComponent("test")

	// Parent records 4 log calls (Debug/Info/Warn/Error).
	// WithFields/WithComponent create child loggers, so they are not recorded on parent.
	if len(m.calls) != 4 {
		t.Fatalf("expected 4 calls on parent, got %d: %+v", len(m.calls), m.calls)
	}
	if m.calls[0].method != "Debug" || m.calls[0].msg != "d" {
		t.Errorf("call[0] = %+v, want Debug/d", m.calls[0])
	}
	if m.calls[1].method != "Info" {
		t.Errorf("call[1] = %+v, want Info", m.calls[1])
	}
	if m.calls[2].method != "Warn" {
		t.Errorf("call[2] = %+v, want Warn", m.calls[2])
	}
	if m.calls[3].method != "Error" {
		t.Errorf("call[3] = %+v, want Error", m.calls[3])
	}

	// Verify children are separate Logger instances.
	if child == nil || child2 == nil {
		t.Fatal("WithFields/WithComponent must return non-nil Logger")
	}

	// Verify child loggers received their respective init calls.
	childMock := child.(*mockLogger)
	if len(childMock.calls) != 1 || childMock.calls[0].method != "WithFields" {
		t.Errorf("child calls = %+v, want WithFields", childMock.calls)
	}
	child2Mock := child2.(*mockLogger)
	if len(child2Mock.calls) != 1 || child2Mock.calls[0].method != "WithComponent" {
		t.Errorf("child2 calls = %+v, want WithComponent", child2Mock.calls)
	}
}
