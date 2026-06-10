package logger

// MultiWriter fans out log writes to multiple LogWriter instances.
// If any writer returns an error, writing continues to remaining writers.
// The first error encountered is returned.
type MultiWriter struct {
	writers []LogWriter
}

// NewMultiWriter creates a MultiWriter from zero or more LogWriter instances.
func NewMultiWriter(writers ...LogWriter) *MultiWriter {
	return &MultiWriter{writers: writers}
}

// Write writes the payload to all writers. Returns the first error encountered.
func (mw *MultiWriter) Write(level Level, payload []byte) (int, error) {
	var firstErr error
	var n int
	for _, w := range mw.writers {
		written, err := w.Write(level, payload)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if written > n {
			n = written
		}
	}
	return n, firstErr
}

// Close closes all writers. Returns the first error encountered.
func (mw *MultiWriter) Close() error {
	var firstErr error
	for _, w := range mw.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
