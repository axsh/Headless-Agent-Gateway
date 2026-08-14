package config

import (
	"testing"
)

func TestIsEmbeddingMode(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{mode: "", want: false},
		{mode: "chat", want: false},
		{mode: "responses", want: false},
		{mode: "embedding", want: true},
		{mode: "Embedding", want: false}, // exact match only
		{mode: "EMBEDDING", want: false},
	}
	for _, tt := range tests {
		if got := IsEmbeddingMode(tt.mode); got != tt.want {
			t.Errorf("IsEmbeddingMode(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestEffectiveMode(t *testing.T) {
	if got := EffectiveMode(""); got != ModelModeChat {
		t.Errorf("EffectiveMode(\"\") = %q, want %q", got, ModelModeChat)
	}
	if got := EffectiveMode(ModelModeEmbedding); got != ModelModeEmbedding {
		t.Errorf("EffectiveMode(embedding) = %q, want %q", got, ModelModeEmbedding)
	}
}

func TestListModelRefs_EmbeddingOnly(t *testing.T) {
	cfg := mixedModeProfiles()
	refs := cfg.ListModelRefs(ModelModeEmbedding)
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	for _, r := range refs {
		if r.Mode != ModelModeEmbedding {
			t.Errorf("got mode %q, want embedding", r.Mode)
		}
	}
}

func TestListModelRefs_ExcludesEmbedding(t *testing.T) {
	cfg := mixedModeProfiles()
	refs := cfg.ListModelRefs("")
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2 (chat+responses)", len(refs))
	}
	for _, r := range refs {
		if IsEmbeddingMode(r.Mode) {
			t.Errorf("agent list included embedding model %q", r.Model)
		}
	}
}

func TestValidate_UnknownMode(t *testing.T) {
	cfg := &ModelProfilesConfig{
		Providers: map[string]ProviderConfig{
			"openai": {
				ApiKeys: []KeyConfig{{
					Name: "default",
					Models: []ModelConfig{
						{Name: "bad-model", Mode: "weird"},
					},
				}},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() = nil, want error for unknown mode")
	}
}

func mixedModeProfiles() *ModelProfilesConfig {
	return &ModelProfilesConfig{
		Providers: map[string]ProviderConfig{
			"openai": {
				ApiKeys: []KeyConfig{{
					Name: "default",
					Models: []ModelConfig{
						{Name: "gpt-4o-mini"},
						{Name: "gpt-4o", Mode: ModelModeResponses},
						{Name: "text-embedding-3-small", Mode: ModelModeEmbedding},
					},
				}},
			},
			"ollama": {
				ApiKeys: []KeyConfig{{
					Name: "default",
					Models: []ModelConfig{
						{Name: "nomic-embed-text", Mode: ModelModeEmbedding},
					},
				}},
			},
		},
	}
}
