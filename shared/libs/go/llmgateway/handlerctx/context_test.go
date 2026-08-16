package handlerctx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

type recLogEntry struct {
	level string
	msg   string
	kv    []any
}

type recLogger struct {
	mu      sync.Mutex
	entries []recLogEntry
}

func (l *recLogger) append(level, msg string, fields []any) {
	copied := append([]any(nil), fields...)
	l.mu.Lock()
	l.entries = append(l.entries, recLogEntry{level: level, msg: msg, kv: copied})
	l.mu.Unlock()
}

func (l *recLogger) Trace(msg string, fields ...any) { l.append("trace", msg, fields) }
func (l *recLogger) Debug(msg string, fields ...any) { l.append("debug", msg, fields) }
func (l *recLogger) Info(msg string, fields ...any)  { l.append("info", msg, fields) }
func (l *recLogger) Warn(msg string, fields ...any)  { l.append("warn", msg, fields) }
func (l *recLogger) Error(msg string, fields ...any) { l.append("error", msg, fields) }
func (l *recLogger) WithFields(map[string]any) logger.Logger {
	return l
}
func (l *recLogger) WithComponent(string) logger.Logger { return l }

func kvString(kv []any, key string) (string, bool) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if ok && k == key {
			return fmt.Sprint(kv[i+1]), true
		}
	}
	return "", false
}

func TestWriteErrorResponseLogs(t *testing.T) {
	rec := httptest.NewRecorder()
	log := &recLogger{}
	WriteErrorResponse(rec, &GatewayError{
		Type:    "not_found_error",
		Message: "model not found: no-such-model",
		Code:    "model_not_found",
		Status:  http.StatusNotFound,
	}, log, "path", "/v1/responses", "method", http.MethodPost, "model", "no-such-model")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if body.Error.Type != "not_found_error" || body.Error.Code != "model_not_found" {
		t.Fatalf("JSON error = %+v", body.Error)
	}
	if body.Error.Message != "model not found: no-such-model" {
		t.Fatalf("message = %q", body.Error.Message)
	}

	var hit recLogEntry
	found := false
	log.mu.Lock()
	for _, e := range log.entries {
		if e.level == "error" && e.msg == LogLLMGatewayErrorResponse {
			hit = e
			found = true
			break
		}
	}
	log.mu.Unlock()
	if !found {
		t.Fatalf("missing ERROR %q", LogLLMGatewayErrorResponse)
	}
	checks := map[string]string{
		"status":  "404",
		"code":    "model_not_found",
		"type":    "not_found_error",
		"message": "model not found: no-such-model",
		"path":    "/v1/responses",
		"method":  http.MethodPost,
		"model":   "no-such-model",
	}
	for k, want := range checks {
		got, ok := kvString(hit.kv, k)
		if !ok || got != want {
			t.Errorf("field %s = %q ok=%v, want %q", k, got, ok, want)
		}
	}
}

func TestWriteErrorResponseNilLogger(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErrorResponse(rec, &GatewayError{
		Type:    "api_error",
		Message: "failed to resolve API key from vault",
		Code:    "vault_error",
		Status:  http.StatusInternalServerError,
	}, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
