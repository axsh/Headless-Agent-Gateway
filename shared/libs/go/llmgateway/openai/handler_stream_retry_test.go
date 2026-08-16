package openai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

type streamTestCtx struct {
	cfg *config.AppConfig
	log logger.Logger
}

func (c *streamTestCtx) Config() *config.AppConfig                             { return c.cfg }
func (c *streamTestCtx) Logger() logger.Logger                                 { return c.log }
func (c *streamTestCtx) Vault() vault.VaultStore                               { return nil }
func (c *streamTestCtx) Router() handlerctx.ModelRouter                         { return nil }
func (c *streamTestCtx) BifrostSDK() *bifrost.Bifrost                          { return nil }
func (c *streamTestCtx) ToBifrostProvider(string) bifrostSchemas.ModelProvider { return "" }
func (c *streamTestCtx) SanitizeTools(*bifrostSchemas.BifrostResponsesRequest, bifrostSchemas.ModelProvider) {
}
func (c *streamTestCtx) TryFallbackAnthropicResponse([]byte) ([]byte, bool) { return nil, false }
func (c *streamTestCtx) ExtractSessionID(string) string                      { return "" }
func (c *streamTestCtx) ExtractFallbackFlag(string) bool                     { return false }
func (c *streamTestCtx) MaskSecret(string) string                            { return "" }

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

func TestHandleResponsesStream_RetryOpenThenSuccess(t *testing.T) {
	orig := openBifrostResponsesStream
	defer func() { openBifrostResponsesStream = orig }()

	var calls int32
	openBifrostResponsesStream = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return nil, &bifrostSchemas.BifrostError{
				Error: &bifrostSchemas.ErrorField{
					Message: "We're currently experiencing high demand",
				},
			}
		}
		ch := make(chan *bifrostSchemas.BifrostStreamChunk, 2)
		ch <- &bifrostSchemas.BifrostStreamChunk{
			BifrostResponsesStreamResponse: &bifrostSchemas.BifrostResponsesStreamResponse{
				Type: "response.created",
			},
		}
		close(ch)
		return ch, nil
	}

	appCfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Retry: config.RetrySettings{
				MaxRetries:          2,
				InitialDelaySeconds: 0,
				MaxDelaySeconds:     8,
			},
		},
	}
	ctx := &streamTestCtx{cfg: appCfg, log: logger.NewDefault(logger.LevelInfo)}
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	handleResponsesStream(ctx, rec, context.Background(), nil, &bifrostSchemas.BifrostResponsesRequest{
		Model: "gpt-4o",
	})

	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("calls = %d, want 2", c)
	}
	body := rec.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("unexpected event: error in stream: %s", body)
	}
	if !strings.Contains(body, "event: response.created") {
		t.Fatalf("missing response.created event: %s", body)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
}

func TestHandleResponsesStream_RetryLeadingErrorChunk(t *testing.T) {
	orig := openBifrostResponsesStream
	defer func() { openBifrostResponsesStream = orig }()

	var calls int32
	openBifrostResponsesStream = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
		n := atomic.AddInt32(&calls, 1)
		ch := make(chan *bifrostSchemas.BifrostStreamChunk, 2)
		if n == 1 {
			ch <- &bifrostSchemas.BifrostStreamChunk{
				BifrostError: &bifrostSchemas.BifrostError{
					Error: &bifrostSchemas.ErrorField{
						Message: "We're currently experiencing high demand",
					},
				},
			}
			close(ch)
			return ch, nil
		}
		ch <- &bifrostSchemas.BifrostStreamChunk{
			BifrostResponsesStreamResponse: &bifrostSchemas.BifrostResponsesStreamResponse{
				Type: "response.completed",
			},
		}
		close(ch)
		return ch, nil
	}

	appCfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Retry: config.RetrySettings{
				MaxRetries:          2,
				InitialDelaySeconds: 0,
				MaxDelaySeconds:     8,
			},
		},
	}
	ctx := &streamTestCtx{cfg: appCfg, log: logger.NewDefault(logger.LevelInfo)}
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	handleResponsesStream(ctx, rec, context.Background(), nil, &bifrostSchemas.BifrostResponsesRequest{
		Model: "gpt-4o",
	})

	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("calls = %d, want 2", c)
	}
	body := rec.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("unexpected leading event: error in stream: %s", body)
	}
	if !strings.Contains(body, "event: response.completed") {
		t.Fatalf("missing response.completed event: %s", body)
	}
}

func TestHandleResponsesStream_NonRetryableNoRetry(t *testing.T) {
	orig := openBifrostResponsesStream
	defer func() { openBifrostResponsesStream = orig }()

	var calls int32
	openBifrostResponsesStream = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
		atomic.AddInt32(&calls, 1)
		return nil, &bifrostSchemas.BifrostError{
			Error: &bifrostSchemas.ErrorField{
				Message: "invalid api key",
			},
		}
	}

	appCfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Retry: config.RetrySettings{
				MaxRetries:          2,
				InitialDelaySeconds: 0,
				MaxDelaySeconds:     8,
			},
		},
	}
	ctx := &streamTestCtx{cfg: appCfg, log: logger.NewDefault(logger.LevelInfo)}
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	handleResponsesStream(ctx, rec, context.Background(), nil, &bifrostSchemas.BifrostResponsesRequest{
		Model: "gpt-4o",
	})

	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("calls = %d, want 1", c)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestHandleResponsesStream_ZeroRetryConfigStillRetries(t *testing.T) {
	orig := openBifrostResponsesStream
	defer func() { openBifrostResponsesStream = orig }()

	var calls int32
	openBifrostResponsesStream = func(hctx handlerctx.HandlerContext, bCtx *bifrostSchemas.BifrostContext, req *bifrostSchemas.BifrostResponsesRequest) (chan *bifrostSchemas.BifrostStreamChunk, *bifrostSchemas.BifrostError) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return nil, &bifrostSchemas.BifrostError{
				Error: &bifrostSchemas.ErrorField{
					Message: "We're currently experiencing high demand",
				},
			}
		}
		ch := make(chan *bifrostSchemas.BifrostStreamChunk, 2)
		ch <- &bifrostSchemas.BifrostStreamChunk{
			BifrostResponsesStreamResponse: &bifrostSchemas.BifrostResponsesStreamResponse{
				Type: "response.created",
			},
		}
		close(ch)
		return ch, nil
	}

	// AppConfig with zero retry settings
	appCfg := &config.AppConfig{}
	ctx := &streamTestCtx{cfg: appCfg, log: logger.NewDefault(logger.LevelInfo)}
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	handleResponsesStream(ctx, rec, ctxTimeout, nil, &bifrostSchemas.BifrostResponsesRequest{
		Model: "gpt-4o",
	})

	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("calls = %d, want 2 (zero config must apply default retries)", c)
	}
}

func TestHandlerSource_StreamRetryWiring(t *testing.T) {
	data, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}
	for _, symbol := range []string{"NewRetryBudget", "RetryLeadingChunk", "openResponsesStream", "openBifrostResponsesStream"} {
		if !bytes.Contains(data, []byte(symbol)) {
			t.Errorf("handler.go does not contain required retry symbol: %s", symbol)
		}
	}
}
