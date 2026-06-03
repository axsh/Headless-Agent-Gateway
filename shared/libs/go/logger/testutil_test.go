package logger

import "bytes"

// bufferWriter is a test helper that captures log output in a bytes.Buffer.
// It implements LogWriter interface.
type bufferWriter struct {
	buf *bytes.Buffer
}

func newBufferWriter() *bufferWriter {
	return &bufferWriter{buf: &bytes.Buffer{}}
}

func (w *bufferWriter) Write(level Level, payload []byte) (int, error) {
	return w.buf.Write(payload)
}

func (w *bufferWriter) Close() error { return nil }

func (w *bufferWriter) String() string {
	return w.buf.String()
}

func (w *bufferWriter) Reset() {
	w.buf.Reset()
}

// mockLogger is a test helper that records calls to Logger methods.
type mockLogger struct {
	calls []mockCall
}

type mockCall struct {
	method string
	msg    string
	fields []any
}

func newMockLogger() *mockLogger {
	return &mockLogger{}
}

func (m *mockLogger) Debug(msg string, fields ...any) {
	m.calls = append(m.calls, mockCall{method: "Debug", msg: msg, fields: fields})
}

func (m *mockLogger) Info(msg string, fields ...any) {
	m.calls = append(m.calls, mockCall{method: "Info", msg: msg, fields: fields})
}

func (m *mockLogger) Warn(msg string, fields ...any) {
	m.calls = append(m.calls, mockCall{method: "Warn", msg: msg, fields: fields})
}

func (m *mockLogger) Error(msg string, fields ...any) {
	m.calls = append(m.calls, mockCall{method: "Error", msg: msg, fields: fields})
}

func (m *mockLogger) WithFields(fields map[string]any) Logger {
	child := newMockLogger()
	child.calls = append(child.calls, mockCall{method: "WithFields"})
	return child
}

func (m *mockLogger) WithComponent(name string) Logger {
	child := newMockLogger()
	child.calls = append(child.calls, mockCall{method: "WithComponent", msg: name})
	return child
}
