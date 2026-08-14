package openai

import (
	"bytes"
	"encoding/json"
	"errors"
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

type mockRouter struct {
	models map[string]*handlerctx.RoutedModel
	err    error
}

func (m *mockRouter) ResolveModel(modelName string, _ string) (*handlerctx.RoutedModel, error) {
	if m.err != nil {
		return nil, m.err
	}
	if r, ok := m.models[modelName]; ok {
		return r, nil
	}
	return nil, errors.New("model not found")
}

type mockHandlerCtx struct {
	cfg    *config.AppConfig
	log    logger.Logger
	router handlerctx.ModelRouter
	sdk    *bifrost.Bifrost
}

func (m *mockHandlerCtx) Config() *config.AppConfig { return m.cfg }
func (m *mockHandlerCtx) Logger() logger.Logger {
	if m.log == nil {
		return logger.NewDefault(logger.LevelError)
	}
	return m.log
}
func (m *mockHandlerCtx) Vault() vault.VaultStore                          { return nil }
func (m *mockHandlerCtx) Router() handlerctx.ModelRouter                   { return m.router }
func (m *mockHandlerCtx) BifrostSDK() *bifrost.Bifrost                      { return m.sdk }
func (m *mockHandlerCtx) ToBifrostProvider(p string) bifrostSchemas.ModelProvider {
	return bifrostSchemas.ModelProvider(p)
}
func (m *mockHandlerCtx) SanitizeTools(*bifrostSchemas.BifrostResponsesRequest, bifrostSchemas.ModelProvider) {
}
func (m *mockHandlerCtx) TryFallbackAnthropicResponse(body []byte) ([]byte, bool) {
	return body, false
}
func (m *mockHandlerCtx) ExtractSessionID(string) string   { return "" }
func (m *mockHandlerCtx) ExtractFallbackFlag(string) bool  { return false }
func (m *mockHandlerCtx) MaskSecret(s string) string       { return "***" }

func embeddingModel(provider, name string) *handlerctx.RoutedModel {
	return &handlerctx.RoutedModel{
		Provider: provider,
		KeyName:  "default",
		KeyValue: "sk-test",
		Model:    name,
		Mode:     config.ModelModeEmbedding,
	}
}

func chatModel(provider, name string) *handlerctx.RoutedModel {
	return &handlerctx.RoutedModel{
		Provider: provider,
		KeyName:  "default",
		KeyValue: "sk-test",
		Model:    name,
		Mode:     "",
	}
}

func withStubInvoker(t *testing.T, fn func(
	ctx handlerctx.HandlerContext,
	bCtx *bifrostSchemas.BifrostContext,
	req *bifrostSchemas.BifrostEmbeddingRequest,
) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError)) {
	t.Helper()
	prev := embeddingInvokerOverride
	embeddingInvokerOverride = fn
	t.Cleanup(func() { embeddingInvokerOverride = prev })
}

func floatEmbedding(values ...float64) bifrostSchemas.EmbeddingStruct {
	return bifrostSchemas.EmbeddingStruct{EmbeddingArray: values}
}

func okEmbeddingResponse(model string, dims int, count int) *bifrostSchemas.BifrostEmbeddingResponse {
	data := make([]bifrostSchemas.EmbeddingData, count)
	vec := make([]float64, dims)
	for i := range vec {
		vec[i] = 0.1 * float64(i+1)
	}
	for i := 0; i < count; i++ {
		data[i] = bifrostSchemas.EmbeddingData{
			Index:     i,
			Object:    "embedding",
			Embedding: floatEmbedding(vec...),
		}
	}
	return &bifrostSchemas.BifrostEmbeddingResponse{
		Object: "list",
		Model:  model,
		Data:   data,
		Usage:  &bifrostSchemas.BifrostLLMUsage{PromptTokens: 3, TotalTokens: 3},
	}
}

func doEmbeddings(t *testing.T, ctx handlerctx.HandlerContext, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleEmbeddings(ctx)(w, req)
	return w
}

func TestHandleEmbeddings_SingleText(t *testing.T) {
	withStubInvoker(t, func(_ handlerctx.HandlerContext, _ *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostEmbeddingRequest) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError) {
		if req.Input == nil || req.Input.Text == nil || *req.Input.Text != "hello" {
			t.Fatalf("unexpected input: %+v", req.Input)
		}
		return okEmbeddingResponse(req.Model, 3, 1), nil
	})
	ctx := &mockHandlerCtx{
		cfg: &config.AppConfig{},
		router: &mockRouter{models: map[string]*handlerctx.RoutedModel{
			"text-embedding-3-small": embeddingModel("openai", "text-embedding-3-small"),
		}},
	}
	w := doEmbeddings(t, ctx, `{"model":"text-embedding-3-small","input":"hello"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp bifrostSchemas.BifrostEmbeddingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding.EmbeddingArray) == 0 {
		t.Fatalf("expected non-empty embedding, got %+v", resp.Data)
	}
}

func TestHandleEmbeddings_BatchTexts(t *testing.T) {
	withStubInvoker(t, func(_ handlerctx.HandlerContext, _ *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostEmbeddingRequest) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError) {
		if len(req.Input.Texts) != 2 {
			t.Fatalf("want 2 texts, got %v", req.Input.Texts)
		}
		return okEmbeddingResponse(req.Model, 2, 2), nil
	})
	ctx := &mockHandlerCtx{
		cfg: &config.AppConfig{},
		router: &mockRouter{models: map[string]*handlerctx.RoutedModel{
			"text-embedding-3-small": embeddingModel("openai", "text-embedding-3-small"),
		}},
	}
	w := doEmbeddings(t, ctx, `{"model":"text-embedding-3-small","input":["a","b"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp bifrostSchemas.BifrostEmbeddingResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 2 {
		t.Fatalf("data len = %d, want 2", len(resp.Data))
	}
	if resp.Data[0].Index != 0 || resp.Data[1].Index != 1 {
		t.Fatalf("unexpected indexes: %+v", resp.Data)
	}
}

func TestHandleEmbeddings_MissingInput(t *testing.T) {
	ctx := &mockHandlerCtx{cfg: &config.AppConfig{}, router: &mockRouter{models: map[string]*handlerctx.RoutedModel{}}}
	w := doEmbeddings(t, ctx, `{"model":"text-embedding-3-small"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleEmbeddings_InvalidJSON(t *testing.T) {
	ctx := &mockHandlerCtx{cfg: &config.AppConfig{}, router: &mockRouter{}}
	w := doEmbeddings(t, ctx, `{`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleEmbeddings_ModelNotFound(t *testing.T) {
	ctx := &mockHandlerCtx{cfg: &config.AppConfig{}, router: &mockRouter{models: map[string]*handlerctx.RoutedModel{}}}
	w := doEmbeddings(t, ctx, `{"model":"missing","input":"hi"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleEmbeddings_WrongMode(t *testing.T) {
	ctx := &mockHandlerCtx{
		cfg: &config.AppConfig{},
		router: &mockRouter{models: map[string]*handlerctx.RoutedModel{
			"gpt-4o-mini": chatModel("openai", "gpt-4o-mini"),
		}},
	}
	w := doEmbeddings(t, ctx, `{"model":"gpt-4o-mini","input":"hi"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("invalid_model_mode")) {
		t.Fatalf("body missing invalid_model_mode: %s", w.Body.String())
	}
}

func TestHandleEmbeddings_UpstreamError(t *testing.T) {
	status := http.StatusBadGateway
	withStubInvoker(t, func(_ handlerctx.HandlerContext, _ *bifrostSchemas.BifrostContext, _ *bifrostSchemas.BifrostEmbeddingRequest) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError) {
		return nil, &bifrostSchemas.BifrostError{
			StatusCode: &status,
			Error:      &bifrostSchemas.ErrorField{Message: "upstream boom"},
		}
	})
	ctx := &mockHandlerCtx{
		cfg: &config.AppConfig{},
		router: &mockRouter{models: map[string]*handlerctx.RoutedModel{
			"text-embedding-3-small": embeddingModel("openai", "text-embedding-3-small"),
		}},
	}
	w := doEmbeddings(t, ctx, `{"model":"text-embedding-3-small","input":"hi"}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestHandleEmbeddings_Providers(t *testing.T) {
	providers := []string{"openai", "ollama", "google"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			modelName := provider + "-embed"
			var gotProvider bifrostSchemas.ModelProvider
			withStubInvoker(t, func(_ handlerctx.HandlerContext, _ *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostEmbeddingRequest) (*bifrostSchemas.BifrostEmbeddingResponse, *bifrostSchemas.BifrostError) {
				gotProvider = req.Provider
				return okEmbeddingResponse(req.Model, 2, 1), nil
			})
			ctx := &mockHandlerCtx{
				cfg: &config.AppConfig{},
				router: &mockRouter{models: map[string]*handlerctx.RoutedModel{
					modelName: embeddingModel(provider, modelName),
				}},
			}
			w := doEmbeddings(t, ctx, `{"model":"`+modelName+`","input":"hi"}`)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
			if string(gotProvider) != provider {
				t.Fatalf("provider = %q, want %q", gotProvider, provider)
			}
		})
	}
}

func TestParseEmbeddingInput(t *testing.T) {
	in, err := parseEmbeddingInput(json.RawMessage(`"x"`))
	if err != nil || in.Text == nil || *in.Text != "x" {
		t.Fatalf("single: %+v err=%v", in, err)
	}
	in, err = parseEmbeddingInput(json.RawMessage(`["a","b"]`))
	if err != nil || len(in.Texts) != 2 {
		t.Fatalf("batch: %+v err=%v", in, err)
	}
	if _, err := parseEmbeddingInput(nil); err == nil {
		t.Fatal("expected error for nil input")
	}
}
