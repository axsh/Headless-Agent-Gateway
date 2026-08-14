// embeddings-client demonstrates Tern's Embeddings Client API, which bypasses
// Coding Agents and talks to LLMGP directly.
//
// Prerequisites:
//   - A running tern server with embedding models in model_profiles.yaml
//     (mode: embedding), e.g. text-embedding-3-small
//   - Provider API keys registered when required (OpenAI / Google)
//
// Usage:
//
//	go run . [server-url] [model-name]
//
// Examples:
//
//	go run .
//	go run . http://localhost:3100 text-embedding-3-small
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	client "github.com/axsh/arctic-tern/client/v1"
)

func main() {
	serverURL := "http://localhost:3100"
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}

	ctx := context.Background()
	c := client.New(serverURL)

	models, err := c.ListEmbeddingModels(ctx)
	if err != nil {
		log.Fatalf("list embedding models: %v", err)
	}
	log.Printf("embedding models: %d", len(models.Models))
	for _, m := range models.Models {
		log.Printf("  - %s/%s", m.Provider, m.Model)
	}

	modelName := ""
	if len(os.Args) > 2 {
		modelName = os.Args[2]
	} else if len(models.Models) > 0 {
		modelName = models.Models[0].Model
	}
	if modelName == "" {
		log.Fatal("no embedding model available; register mode: embedding in model_profiles.yaml")
	}

	resp, err := c.CreateEmbedding(ctx, client.EmbeddingRequest{
		Model: modelName,
		Input: "hello embeddings",
	})
	if err != nil {
		log.Fatalf("create embedding: %v", err)
	}
	if len(resp.Data) == 0 {
		log.Fatal("empty embedding response")
	}
	fmt.Printf("model=%s dims=%d\n", resp.Model, len(resp.Data[0].Embedding))
}
