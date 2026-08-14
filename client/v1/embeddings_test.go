package v1

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateEmbedding_Single(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/embeddings" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2,0.3],"index":0}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.CreateEmbedding(context.Background(), EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: "hello",
	})
	if err != nil {
		t.Fatalf("CreateEmbedding: %v", err)
	}
	if gotBody["input"] != "hello" {
		t.Fatalf("input = %v", gotBody["input"])
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 3 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateEmbedding_Batch(t *testing.T) {
	var bodyBytes []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0},{"object":"embedding","embedding":[0.2],"index":1}],"model":"m"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreateEmbedding(context.Background(), EmbeddingRequest{
		Model: "m",
		Input: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("CreateEmbedding: %v", err)
	}
	if !strings.Contains(string(bodyBytes), `"a"`) || !strings.Contains(string(bodyBytes), `"b"`) {
		t.Fatalf("batch input not encoded: %s", bodyBytes)
	}
}

func TestCreateEmbedding_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad"}}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	_, err := c.CreateEmbedding(context.Background(), EmbeddingRequest{Model: "m", Input: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListEmbeddingModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/embeddings/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"models":[{"provider":"openai","model":"text-embedding-3-small"}]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	resp, err := c.ListEmbeddingModels(context.Background())
	if err != nil {
		t.Fatalf("ListEmbeddingModels: %v", err)
	}
	if len(resp.Models) != 1 || resp.Models[0].Model != "text-embedding-3-small" {
		t.Fatalf("unexpected: %+v", resp)
	}
}
