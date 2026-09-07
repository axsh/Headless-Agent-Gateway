package openai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	bifrost "github.com/maximhq/bifrost/core"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

type mockLogger struct{}

func (m *mockLogger) Trace(string, ...any)                     {}
func (m *mockLogger) Debug(string, ...any)                     {}
func (m *mockLogger) Info(string, ...any)                      {}
func (m *mockLogger) Warn(string, ...any)                      {}
func (m *mockLogger) Error(string, ...any)                     {}
func (m *mockLogger) WithFields(map[string]any) logger.Logger  { return m }
func (m *mockLogger) WithComponent(string) logger.Logger       { return m }

type reasoningMockRouter struct {
	routed *handlerctx.RoutedModel
}

func (r *reasoningMockRouter) ResolveModel(string, string) (*handlerctx.RoutedModel, error) {
	return r.routed, nil
}

type reasoningTestCtx struct {
	cfg    *config.AppConfig
	log    logger.Logger
	router handlerctx.ModelRouter
	sdk    *bifrost.Bifrost
}

func (c *reasoningTestCtx) Config() *config.AppConfig                             { return c.cfg }
func (c *reasoningTestCtx) Logger() logger.Logger                                 { return c.log }
func (c *reasoningTestCtx) Vault() vault.VaultStore                               { return nil }
func (c *reasoningTestCtx) Router() handlerctx.ModelRouter                        { return c.router }
func (c *reasoningTestCtx) BifrostSDK() *bifrost.Bifrost                          { return c.sdk }
func (c *reasoningTestCtx) ToBifrostProvider(string) bifrostSchemas.ModelProvider { return "openai" }
func (c *reasoningTestCtx) SanitizeTools(*bifrostSchemas.BifrostResponsesRequest, bifrostSchemas.ModelProvider) {
}
func (c *reasoningTestCtx) TryFallbackAnthropicResponse([]byte) ([]byte, bool) { return nil, false }
func (c *reasoningTestCtx) ExtractSessionID(string) string                     { return "" }
func (c *reasoningTestCtx) ExtractFallbackFlag(string) bool                    { return false }
func (c *reasoningTestCtx) MaskSecret(string) string                           { return "" }

func newAstraRoutedModel() *handlerctx.RoutedModel {
	return &handlerctx.RoutedModel{
		Provider: "openai",
		Model:    "gpt-6-astra",
		Mode:     "responses",
		Reasoning: &config.ModelReasoning{
			Required:         true,
			SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
			DefaultEffort:    "medium",
		},
	}
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (status int, typ, msg, code string) {
	t.Helper()
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode error body: %v (raw: %s)", err, rec.Body.String())
	}
	return rec.Code, body.Error.Type, body.Error.Message, body.Error.Code
}

func TestHandleResponses_Reasoning_UnsupportedEffort_Returns400(t *testing.T) {
	hctx := &reasoningTestCtx{
		cfg:    &config.AppConfig{},
		log:    &mockLogger{},
		router: &reasoningMockRouter{routed: newAstraRoutedModel()},
		sdk:    &bifrost.Bifrost{},
	}

	// Send effort: "none" to gpt-6-astra
	payload := `{"model":"gpt-6-astra","reasoning":{"effort":"none"},"input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(payload)))
	rec := httptest.NewRecorder()

	HandleResponses(hctx)(rec, req)

	status, typ, _, code := decodeError(t, rec)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if typ != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", typ)
	}
	if code != "unsupported_reasoning_effort" {
		t.Errorf("code = %q, want unsupported_reasoning_effort", code)
	}
}

func TestHandleResponses_Reasoning_UnknownEffort_Returns400(t *testing.T) {
	hctx := &reasoningTestCtx{
		cfg:    &config.AppConfig{},
		log:    &mockLogger{},
		router: &reasoningMockRouter{routed: newAstraRoutedModel()},
		sdk:    &bifrost.Bifrost{},
	}

	payload := `{"model":"gpt-6-astra","reasoning":{"effort":"super-fast"},"input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(payload)))
	rec := httptest.NewRecorder()

	HandleResponses(hctx)(rec, req)

	status, typ, _, code := decodeError(t, rec)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if typ != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", typ)
	}
	if code != "unsupported_reasoning_effort" {
		t.Errorf("code = %q, want unsupported_reasoning_effort", code)
	}
}

func TestHandleResponses_Reasoning_MissingRequiredWithoutDefault_Returns400(t *testing.T) {
	routed := newAstraRoutedModel()
	routed.Reasoning.DefaultEffort = "" // no default configured

	hctx := &reasoningTestCtx{
		cfg:    &config.AppConfig{},
		log:    &mockLogger{},
		router: &reasoningMockRouter{routed: routed},
		sdk:    &bifrost.Bifrost{},
	}

	payload := `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(payload)))
	rec := httptest.NewRecorder()

	HandleResponses(hctx)(rec, req)

	status, typ, _, code := decodeError(t, rec)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if typ != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", typ)
	}
	if code != "missing_reasoning_effort" {
		t.Errorf("code = %q, want missing_reasoning_effort", code)
	}
}

func TestHandleResponses_Reasoning_BackfillDefaultEffort(t *testing.T) {
	orig := openBifrostResponsesStream
	defer func() { openBifrostResponsesStream = orig }()

	var capturedReq *bifrostSchemas.BifrostResponsesRequest
	openBifrostResponsesStream = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
		capturedReq = req
		ch := make(chan *bifrostSchemas.BifrostStreamChunk)
		close(ch)
		return ch, nil
	}

	hctx := &reasoningTestCtx{
		cfg:    &config.AppConfig{},
		log:    &mockLogger{},
		router: &reasoningMockRouter{routed: newAstraRoutedModel()},
		sdk:    &bifrost.Bifrost{},
	}

	// Request without reasoning parameter, stream: true
	payload := `{"model":"gpt-6-astra","stream":true,"input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(payload)))
	rec := &flushRecorder{httptest.NewRecorder()}

	HandleResponses(hctx)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if capturedReq == nil {
		t.Fatal("expected request to reach Bifrost")
	}
	if capturedReq.Params == nil || capturedReq.Params.Reasoning == nil || capturedReq.Params.Reasoning.Effort == nil {
		t.Fatal("expected Reasoning.Effort to be backfilled")
	}
	if *capturedReq.Params.Reasoning.Effort != "medium" {
		t.Errorf("Effort = %q, want medium", *capturedReq.Params.Reasoning.Effort)
	}
}

func TestHandleResponses_Reasoning_PreservesProvidedSupportedEffort(t *testing.T) {
	orig := openBifrostResponsesStream
	defer func() { openBifrostResponsesStream = orig }()

	var capturedReq *bifrostSchemas.BifrostResponsesRequest
	openBifrostResponsesStream = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
		capturedReq = req
		ch := make(chan *bifrostSchemas.BifrostStreamChunk)
		close(ch)
		return ch, nil
	}

	hctx := &reasoningTestCtx{
		cfg:    &config.AppConfig{},
		log:    &mockLogger{},
		router: &reasoningMockRouter{routed: newAstraRoutedModel()},
		sdk:    &bifrost.Bifrost{},
	}

	// Request with explicit effort: "high", stream: true
	payload := `{"model":"gpt-6-astra","stream":true,"reasoning":{"effort":"high"},"input":[{"type":"message","role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(payload)))
	rec := &flushRecorder{httptest.NewRecorder()}

	HandleResponses(hctx)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if capturedReq == nil {
		t.Fatal("expected request to reach Bifrost")
	}
	if capturedReq.Params == nil || capturedReq.Params.Reasoning == nil || capturedReq.Params.Reasoning.Effort == nil {
		t.Fatal("expected Reasoning.Effort to exist")
	}
	if *capturedReq.Params.Reasoning.Effort != "high" {
		t.Errorf("Effort = %q, want high", *capturedReq.Params.Reasoning.Effort)
	}
}
