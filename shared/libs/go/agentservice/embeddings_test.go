package agentservice_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

type trackingAgent struct {
	name    string
	creates atomic.Int32
}

func (m *trackingAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	m.creates.Add(1)
	return &mockCodingSession{}, nil
}
func (m *trackingAgent) Name() string { return m.name }
func (m *trackingAgent) Close() error { return nil }

func TestHandleCreateEmbedding_ProxiesToGateway(t *testing.T) {
	var gotPath string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"text-embedding-3-small"}`))
	}))
	defer gw.Close()

	agent := &trackingAgent{name: "claudecode"}
	srv := agentservice.New(agentservice.WithGatewayURL(gw.URL))
	srv.RegisterAgent(agent)
	handler := srv.HTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/embeddings", strings.NewReader(
		`{"model":"text-embedding-3-small","input":"hello"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("gateway path = %q, want /v1/embeddings", gotPath)
	}
	if agent.creates.Load() != 0 {
		t.Fatalf("CreateSession called %d times, want 0", agent.creates.Load())
	}
}

func TestHandleCreateEmbedding_ForwardsAuthToken(t *testing.T) {
	var gotToken string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Gateway-Token")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"object":"list","data":[],"model":"m"}`))
	}))
	defer gw.Close()

	srv := agentservice.New(
		agentservice.WithGatewayURL(gw.URL),
		agentservice.WithGatewayToken("secret-token"),
	)
	handler := srv.HTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/embeddings", strings.NewReader(`{"model":"m","input":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if gotToken != "secret-token" {
		t.Fatalf("token = %q, want secret-token", gotToken)
	}
}

func TestHandleCreateEmbedding_GatewayUnreachable(t *testing.T) {
	srv := agentservice.New(agentservice.WithGatewayURL("http://127.0.0.1:1"))
	handler := srv.HTTPHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/embeddings", strings.NewReader(`{"model":"m","input":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestHandleCreateEmbedding_MissingGateway(t *testing.T) {
	srv := agentservice.New()
	handler := srv.HTTPHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/embeddings", strings.NewReader(`{"model":"m","input":"x"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleCreateEmbedding_MissingInput_Passthrough(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_input","message":"input is required"}}`))
	}))
	defer gw.Close()

	srv := agentservice.New(agentservice.WithGatewayURL(gw.URL))
	handler := srv.HTTPHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/embeddings", strings.NewReader(`{"model":"m"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleListEmbeddingModels(t *testing.T) {
	srv := agentservice.New()
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {
				ApiKeys: []config.KeyConfig{{
					Name: "default",
					Models: []config.ModelConfig{
						{Name: "gpt-4o-mini"},
						{Name: "text-embedding-3-small", Mode: config.ModelModeEmbedding},
					},
				}},
			},
			"ollama": {
				ApiKeys: []config.KeyConfig{{
					Name: "default",
					Models: []config.ModelConfig{
						{Name: "nomic-embed-text", Mode: config.ModelModeEmbedding},
					},
				}},
			},
		},
	})
	handler := srv.HTTPHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/embeddings/models", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Models []llmgateway.ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(body.Models))
	}
	for _, m := range body.Models {
		if m.Model == "gpt-4o-mini" {
			t.Fatal("chat model leaked into embedding list")
		}
	}
}
