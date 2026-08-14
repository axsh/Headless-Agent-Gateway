package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	client "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

type embedTrackingAgent struct {
	creates atomic.Int32
}

func (a *embedTrackingAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	a.creates.Add(1)
	return nil, nil
}
func (a *embedTrackingAgent) Name() string { return "claudecode" }
func (a *embedTrackingAgent) Close() error { return nil }

func embeddingProfiles() *config.ModelProfilesConfig {
	return &config.ModelProfilesConfig{
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
			"google": {
				ApiKeys: []config.KeyConfig{{
					Name: "default",
					Models: []config.ModelConfig{
						{Name: "text-embedding-004", Mode: config.ModelModeEmbedding},
					},
				}},
			},
		},
	}
}

func startEmbeddingStack(t *testing.T, gw http.Handler) (baseURL string, agent *embedTrackingAgent, cleanup func()) {
	t.Helper()
	gwSrv := httptest.NewServer(gw)
	agent = &embedTrackingAgent{}
	as := agentservice.New(agentservice.WithGatewayURL(gwSrv.URL))
	as.RegisterAgent(agent)
	as.SetModelProfiles(embeddingProfiles())
	as.SetGatewayModels(
		[]llmgateway.ModelInfo{{Provider: "openai", Model: "gpt-4o-mini"}},
		&llmgateway.ModelInfo{Provider: "openai", Model: "gpt-4o-mini"},
	)
	asSrv := httptest.NewServer(as.HTTPHandler())
	cleanup = func() {
		asSrv.Close()
		gwSrv.Close()
	}
	return asSrv.URL, agent, cleanup
}

func stubEmbeddingGateway(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if len(req.Input) == 0 || string(req.Input) == "null" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"invalid_input","message":"input is required"}}`))
			return
		}
		if strings.Contains(req.Model, "gpt-") || req.Model == "unknown-model" {
			status := http.StatusBadRequest
			code := "invalid_model_mode"
			if req.Model == "unknown-model" {
				status = http.StatusNotFound
				code = "model_not_found"
			}
			w.WriteHeader(status)
			w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"` + code + `","message":"bad model"}}`))
			return
		}

		count := 1
		var texts []string
		if err := json.Unmarshal(req.Input, &texts); err == nil {
			count = len(texts)
		}
		data := make([]map[string]any, count)
		for i := 0; i < count; i++ {
			data[i] = map[string]any{
				"object":    "embedding",
				"embedding": []float64{0.1, 0.2, 0.3},
				"index":     i,
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  req.Model,
			"data":   data,
		})
	})
}

func TestEmbedding_CreateViaClient_StubGateway(t *testing.T) {
	baseURL, agent, cleanup := startEmbeddingStack(t, stubEmbeddingGateway(t))
	defer cleanup()

	c := client.New(baseURL)
	resp, err := c.CreateEmbedding(context.Background(), client.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: "hello embeddings",
	})
	if err != nil {
		t.Fatalf("CreateEmbedding: %v", err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) == 0 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if agent.creates.Load() != 0 {
		t.Fatalf("Coding Agent CreateSession called %d times", agent.creates.Load())
	}
}

func TestEmbedding_ListModels_SeparatesChatAndEmbedding(t *testing.T) {
	baseURL, _, cleanup := startEmbeddingStack(t, stubEmbeddingGateway(t))
	defer cleanup()

	c := client.New(baseURL)
	chat, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	for _, m := range chat.Models {
		if strings.Contains(m.Model, "embedding") || m.Model == "nomic-embed-text" {
			t.Fatalf("chat ListModels included embedding model %q", m.Model)
		}
	}

	emb, err := c.ListEmbeddingModels(context.Background())
	if err != nil {
		t.Fatalf("ListEmbeddingModels: %v", err)
	}
	if len(emb.Models) != 3 {
		t.Fatalf("embedding models = %d, want 3", len(emb.Models))
	}
	for _, m := range emb.Models {
		if m.Model == "gpt-4o-mini" {
			t.Fatal("chat model in embedding list")
		}
	}
}

func TestEmbedding_BatchInputs(t *testing.T) {
	baseURL, _, cleanup := startEmbeddingStack(t, stubEmbeddingGateway(t))
	defer cleanup()

	c := client.New(baseURL)
	resp, err := c.CreateEmbedding(context.Background(), client.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: []string{"one", "two"},
	})
	if err != nil {
		t.Fatalf("CreateEmbedding: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("data len = %d, want 2", len(resp.Data))
	}
	if resp.Data[0].Index != 0 || resp.Data[1].Index != 1 {
		t.Fatalf("indexes = %d,%d", resp.Data[0].Index, resp.Data[1].Index)
	}
}

func TestEmbedding_InvalidModelMode(t *testing.T) {
	baseURL, _, cleanup := startEmbeddingStack(t, stubEmbeddingGateway(t))
	defer cleanup()

	c := client.New(baseURL)
	_, err := c.CreateEmbedding(context.Background(), client.EmbeddingRequest{
		Model: "gpt-4o-mini",
		Input: "hello",
	})
	if err == nil {
		t.Fatal("expected error for chat model")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("want HTTP 400, got %v", err)
	}
}

func TestEmbedding_UnknownModel(t *testing.T) {
	baseURL, _, cleanup := startEmbeddingStack(t, stubEmbeddingGateway(t))
	defer cleanup()

	c := client.New(baseURL)
	_, err := c.CreateEmbedding(context.Background(), client.EmbeddingRequest{
		Model: "unknown-model",
		Input: "hello",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("want HTTP 404, got %v", err)
	}
}

func TestEmbedding_MultiProviderRouting(t *testing.T) {
	models := []string{"text-embedding-3-small", "nomic-embed-text", "text-embedding-004"}
	baseURL, _, cleanup := startEmbeddingStack(t, stubEmbeddingGateway(t))
	defer cleanup()
	c := client.New(baseURL)

	for _, model := range models {
		resp, err := c.CreateEmbedding(context.Background(), client.EmbeddingRequest{
			Model: model,
			Input: "hello",
		})
		if err != nil {
			t.Fatalf("model %s: %v", model, err)
		}
		if len(resp.Data) == 0 || len(resp.Data[0].Embedding) == 0 {
			t.Fatalf("model %s empty embedding", model)
		}
	}
}
