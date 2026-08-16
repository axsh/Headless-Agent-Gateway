package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	bifrost "github.com/maximhq/bifrost/core"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

type errorLogEntry struct {
	level string
	msg   string
	kv    []any
}

type errorCaptureLog struct {
	mu      sync.Mutex
	entries []errorLogEntry
}

func (l *errorCaptureLog) append(level, msg string, fields []any) {
	copied := append([]any(nil), fields...)
	l.mu.Lock()
	l.entries = append(l.entries, errorLogEntry{level: level, msg: msg, kv: copied})
	l.mu.Unlock()
}

func (l *errorCaptureLog) Trace(msg string, fields ...any) { l.append("trace", msg, fields) }
func (l *errorCaptureLog) Debug(msg string, fields ...any) { l.append("debug", msg, fields) }
func (l *errorCaptureLog) Info(msg string, fields ...any)  { l.append("info", msg, fields) }
func (l *errorCaptureLog) Warn(msg string, fields ...any)  { l.append("warn", msg, fields) }
func (l *errorCaptureLog) Error(msg string, fields ...any) { l.append("error", msg, fields) }
func (l *errorCaptureLog) WithFields(map[string]any) logger.Logger {
	return l
}
func (l *errorCaptureLog) WithComponent(string) logger.Logger { return l }

func (l *errorCaptureLog) has(level, msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.level == level && e.msg == msg {
			return true
		}
	}
	return false
}

func (l *errorCaptureLog) find(level, msg string) (errorLogEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.level == level && e.msg == msg {
			return e, true
		}
	}
	return errorLogEntry{}, false
}

func fieldString(kv []any, key string) string {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if ok && k == key {
			return stringifyField(kv[i+1])
		}
	}
	return "<missing>"
}

func stringifyField(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

type stubRouter struct {
	routed *handlerctx.RoutedModel
	err    error
}

func (s *stubRouter) ResolveModel(string, string) (*handlerctx.RoutedModel, error) {
	return s.routed, s.err
}

type stubVault struct {
	err error
}

func (s *stubVault) Resolve(string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return "sk-test", nil
}
func (s *stubVault) Set(string, string) error { return nil }
func (s *stubVault) Delete(string) error      { return nil }
func (s *stubVault) List() ([]string, error)  { return nil, nil }

type errorLogCtx struct {
	cfg    *config.AppConfig
	log    logger.Logger
	router handlerctx.ModelRouter
	vault  vault.VaultStore
	sdk    *bifrost.Bifrost
}

func (c *errorLogCtx) Config() *config.AppConfig                             { return c.cfg }
func (c *errorLogCtx) Logger() logger.Logger                                 { return c.log }
func (c *errorLogCtx) Vault() vault.VaultStore                               { return c.vault }
func (c *errorLogCtx) Router() handlerctx.ModelRouter                        { return c.router }
func (c *errorLogCtx) BifrostSDK() *bifrost.Bifrost                          { return c.sdk }
func (c *errorLogCtx) ToBifrostProvider(string) bifrostSchemas.ModelProvider { return "" }
func (c *errorLogCtx) SanitizeTools(*bifrostSchemas.BifrostResponsesRequest, bifrostSchemas.ModelProvider) {
}
func (c *errorLogCtx) TryFallbackAnthropicResponse([]byte) ([]byte, bool) { return nil, false }
func (c *errorLogCtx) ExtractSessionID(string) string                     { return "" }
func (c *errorLogCtx) ExtractFallbackFlag(string) bool                    { return false }
func (c *errorLogCtx) MaskSecret(string) string                           { return "" }

func decodeGatewayJSON(t *testing.T, rec *httptest.ResponseRecorder) (typ, msg, code string) {
	t.Helper()
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, rec.Body.String())
	}
	return body.Error.Type, body.Error.Message, body.Error.Code
}

func TestHandleResponses_UnknownModelLogsNotFound(t *testing.T) {
	log := &errorCaptureLog{}
	hctx := &errorLogCtx{
		cfg:    &config.AppConfig{},
		log:    log,
		router: &stubRouter{err: errors.New("no profile")},
	}
	body := bytes.NewBufferString(`{"model":"no-such-model"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
	rec := httptest.NewRecorder()
	handleResponses(hctx, rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	typ, msg, code := decodeGatewayJSON(t, rec)
	if typ != "not_found_error" || code != "model_not_found" {
		t.Fatalf("type=%q code=%q", typ, code)
	}
	if msg != "model not found: no-such-model" {
		t.Fatalf("message = %q", msg)
	}
	if log.has("info", "openai responses request via bifrost") {
		t.Fatal("via bifrost Info must not be logged on 404")
	}
	hit, ok := log.find("error", handlerctx.LogLLMGatewayErrorResponse)
	if !ok {
		t.Fatalf("missing ERROR %q", handlerctx.LogLLMGatewayErrorResponse)
	}
	if fieldString(hit.kv, "code") != "model_not_found" {
		t.Errorf("code field = %s", fieldString(hit.kv, "code"))
	}
	if fieldString(hit.kv, "status") != "404" {
		t.Errorf("status field = %s", fieldString(hit.kv, "status"))
	}
	if fieldString(hit.kv, "path") != "/v1/responses" {
		t.Errorf("path field = %s", fieldString(hit.kv, "path"))
	}
	if fieldString(hit.kv, "model") != "no-such-model" {
		t.Errorf("model field = %s", fieldString(hit.kv, "model"))
	}
}

func TestHandleResponses_VaultResolveFailureLogsVaultError(t *testing.T) {
	log := &errorCaptureLog{}
	hctx := &errorLogCtx{
		cfg: &config.AppConfig{},
		log: log,
		router: &stubRouter{routed: &handlerctx.RoutedModel{
			Provider: "openai",
			Model:    "gpt-4o",
			KeyValue: "vault://providers/openai/default",
			Mode:     "responses",
		}},
		vault: &stubVault{err: errors.New("env backend: TERN_VAULT_OPENAI_DEFAULT unset")},
		sdk:   &bifrost.Bifrost{},
	}
	body := bytes.NewBufferString(`{"model":"gpt-4o"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
	rec := httptest.NewRecorder()
	handleResponses(hctx, rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	_, msg, code := decodeGatewayJSON(t, rec)
	if code != "vault_error" {
		t.Fatalf("code = %q, want vault_error", code)
	}
	if msg != "failed to resolve API key from vault" {
		t.Fatalf("message = %q", msg)
	}
	if log.has("info", "openai responses request via bifrost") {
		t.Fatal("via bifrost Info must not be logged on vault 500")
	}
	hit, ok := log.find("error", handlerctx.LogLLMGatewayErrorResponse)
	if !ok {
		t.Fatalf("missing ERROR %q", handlerctx.LogLLMGatewayErrorResponse)
	}
	if fieldString(hit.kv, "code") != "vault_error" {
		t.Errorf("code field = %s", fieldString(hit.kv, "code"))
	}
	if fieldString(hit.kv, "status") != "500" {
		t.Errorf("status field = %s", fieldString(hit.kv, "status"))
	}
}
