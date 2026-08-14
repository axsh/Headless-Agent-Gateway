package llm_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	client "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

// TestEmbeddingE2E_OpenAI calls a real OpenAI embedding model when a vault key exists.
func TestEmbeddingE2E_OpenAI(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	gwPort := freePort(t)
	asPort := freePort(t)
	wsPort := freePort(t)

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              gwPort,
			ModelProfilesPath: profilesPath,
		},
		AgentService: config.AgentServiceConfig{Port: asPort},
		WebSocket:    config.WebSocketConfig{Port: wsPort},
		Vault:        config.VaultConfig{Backends: []string{"keyring"}},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
		server.WithKeyringVault(),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	if err := srv.AgentService().FetchModelsFromGateway(); err != nil {
		t.Logf("FetchModelsFromGateway warning: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", srv.AgentService().Port())
	c := client.New(baseURL)
	resp, err := c.CreateEmbedding(context.Background(), client.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: "hello embeddings",
	})
	if err != nil {
		t.Fatalf("CreateEmbedding: %v", err)
	}
	if len(resp.Data) == 0 || len(resp.Data[0].Embedding) < 10 {
		t.Fatalf("unexpected embedding response: %+v", resp)
	}
	t.Logf("dims=%d model=%s", len(resp.Data[0].Embedding), resp.Model)
}
