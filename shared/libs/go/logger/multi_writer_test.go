package logger

import (
	"errors"
	"testing"
)

type customMockWriter struct {
	written [][]byte
	levels  []Level
	err     error
	closed  bool
}

func (w *customMockWriter) Write(level Level, payload []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.written = append(w.written, payload)
	w.levels = append(w.levels, level)
	return len(payload), nil
}

func (w *customMockWriter) Close() error {
	w.closed = true
	return nil
}

func TestMultiWriter_WriteToAll(t *testing.T) {
	w1 := &customMockWriter{}
	w2 := &customMockWriter{}
	mw := NewMultiWriter(w1, w2)

	payload := []byte("hello")
	n, err := mw.Write(LevelInfo, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(payload) {
		t.Errorf("expected n to be %d, got %d", len(payload), n)
	}

	if len(w1.written) != 1 || string(w1.written[0]) != "hello" {
		t.Errorf("w1 did not receive correct payload: %v", w1.written)
	}
	if len(w2.written) != 1 || string(w2.written[0]) != "hello" {
		t.Errorf("w2 did not receive correct payload: %v", w2.written)
	}
}

func TestMultiWriter_OneWriterFails(t *testing.T) {
	w1 := &customMockWriter{}
	w2 := &customMockWriter{err: errors.New("write error")}
	w3 := &customMockWriter{}
	mw := NewMultiWriter(w1, w2, w3)

	payload := []byte("hello")
	_, err := mw.Write(LevelInfo, payload)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "write error" {
		t.Errorf("expected 'write error', got %v", err)
	}

	// Should still write to w1 and w3
	if len(w1.written) != 1 || string(w1.written[0]) != "hello" {
		t.Errorf("w1 did not receive payload: %v", w1.written)
	}
	if len(w3.written) != 1 || string(w3.written[0]) != "hello" {
		t.Errorf("w3 did not receive payload: %v", w3.written)
	}
}

func TestMultiWriter_Close(t *testing.T) {
	w1 := &customMockWriter{}
	w2 := &customMockWriter{}
	mw := NewMultiWriter(w1, w2)

	err := mw.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !w1.closed {
		t.Error("w1 not closed")
	}
	if !w2.closed {
		t.Error("w2 not closed")
	}
}

func TestMultiWriter_Empty(t *testing.T) {
	mw := NewMultiWriter()
	payload := []byte("hello")
	n, err := mw.Write(LevelInfo, payload)
	if err != nil {
		t.Fatalf("unexpected error on empty multiwriter: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes written, got %d", n)
	}
	
	err = mw.Close()
	if err != nil {
		t.Fatalf("unexpected error closing empty multiwriter: %v", err)
	}
}
