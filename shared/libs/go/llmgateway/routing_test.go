package llmgateway

import (
	"errors"
	"testing"

	"github.com/axsh/hag/config"
)

// testProfiles returns a ModelProfilesConfig with anthropic and openai providers.
func testProfiles() *config.ModelProfilesConfig {
	return &config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				Keys: []config.KeyConfig{
					{
						Name:  "primary",
						Value: "sk-ant-test-key",
						Models: []config.ModelConfig{
							{Name: "claude-sonnet-4-20250514"},
							{Name: "claude-haiku-3-20240307"},
						},
					},
				},
			},
			"openai": {
				Keys: []config.KeyConfig{
					{
						Name:  "default",
						Value: "sk-openai-test-key",
						Models: []config.ModelConfig{
							{Name: "gpt-4o"},
							{Name: "gpt-4o-mini"},
						},
					},
				},
			},
		},
	}
}

func TestModelRouter_ResolveModel(t *testing.T) {
	tests := []struct {
		name         string
		profiles     *config.ModelProfilesConfig
		modelName    string
		wantProvider string
		wantKeyName  string
		wantModel    string
		wantErr      error
	}{
		{
			name:         "resolve anthropic model",
			profiles:     testProfiles(),
			modelName:    "claude-sonnet-4-20250514",
			wantProvider: "anthropic",
			wantKeyName:  "primary",
			wantModel:    "claude-sonnet-4-20250514",
		},
		{
			name:         "resolve openai model",
			profiles:     testProfiles(),
			modelName:    "gpt-4o",
			wantProvider: "openai",
			wantKeyName:  "default",
			wantModel:    "gpt-4o",
		},
		{
			name:         "resolve second model in same key",
			profiles:     testProfiles(),
			modelName:    "gpt-4o-mini",
			wantProvider: "openai",
			wantKeyName:  "default",
			wantModel:    "gpt-4o-mini",
		},
		{
			name:      "undefined model returns error",
			profiles:  testProfiles(),
			modelName: "nonexistent-model",
			wantErr:   ErrModelNotFound,
		},
		{
			name:      "nil profiles returns error",
			profiles:  nil,
			modelName: "claude-sonnet-4-20250514",
			wantErr:   ErrModelNotFound,
		},
		{
			name:      "empty model name returns error",
			profiles:  testProfiles(),
			modelName: "",
			wantErr:   ErrModelNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewModelRouter(tt.profiles, nil)
			got, err := router.ResolveModel(tt.modelName)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.wantProvider)
			}
			if got.KeyName != tt.wantKeyName {
				t.Errorf("KeyName = %q, want %q", got.KeyName, tt.wantKeyName)
			}
			if got.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", got.Model, tt.wantModel)
			}
			if got.KeyValue == "" {
				t.Error("KeyValue should not be empty")
			}
		})
	}
}
