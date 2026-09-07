package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClient_ListModels_Reasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ModelsResponse{
			Models: []ModelInfo{
				{
					Provider: "openai",
					Model:    "gpt-6-astra",
					Reasoning: &ModelReasoning{
						Required:         true,
						SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
						DefaultEffort:    "medium",
					},
				},
				{
					Provider: "openai",
					Model:    "gpt-4o",
				},
			},
			DefaultModel: &ModelInfo{
				Provider: "openai",
				Model:    "gpt-6-astra",
				Reasoning: &ModelReasoning{
					Required:         true,
					SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
					DefaultEffort:    "medium",
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(resp.Models) != 2 {
		t.Fatalf("len(Models) = %d, want 2", len(resp.Models))
	}
	astra := resp.Models[0]
	if astra.Reasoning == nil {
		t.Fatal("expected gpt-6-astra reasoning not nil")
	}
	if !astra.Reasoning.Required {
		t.Error("expected Required = true")
	}
	if astra.Reasoning.DefaultEffort != "medium" {
		t.Errorf("DefaultEffort = %q, want medium", astra.Reasoning.DefaultEffort)
	}
	wantEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	if !reflect.DeepEqual(astra.Reasoning.SupportedEfforts, wantEfforts) {
		t.Errorf("SupportedEfforts = %v, want %v", astra.Reasoning.SupportedEfforts, wantEfforts)
	}

	gpt4o := resp.Models[1]
	if gpt4o.Reasoning != nil {
		t.Errorf("expected gpt-4o reasoning nil, got %+v", gpt4o.Reasoning)
	}

	if resp.DefaultModel == nil || resp.DefaultModel.Reasoning == nil {
		t.Fatal("expected default_model reasoning not nil")
	}
}
