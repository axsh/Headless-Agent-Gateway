package llmgateway

import (
	"context"
	"testing"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/vault"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestBifrostAccount_GetConfiguredProviders(t *testing.T) {
	tests := []struct {
		name      string
		profiles  *config.ModelProfilesConfig
		wantCount int
		wantNames []bifrostSchemas.ModelProvider
	}{
		{
			name:      "three providers",
			profiles:  testProfiles(),
			wantCount: 3,
			wantNames: []bifrostSchemas.ModelProvider{bifrostSchemas.Anthropic, bifrostSchemas.OpenAI, bifrostSchemas.Gemini},
		},
		{
			name:      "nil profiles",
			profiles:  nil,
			wantCount: 0,
		},
		{
			name: "empty providers map",
			profiles: &config.ModelProfilesConfig{
				Providers: map[string]config.ProviderConfig{},
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := NewBifrostAccount(tt.profiles, nil, nil)
			providers, err := account.GetConfiguredProviders()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(providers) != tt.wantCount {
				t.Fatalf("got %d providers, want %d", len(providers), tt.wantCount)
			}
			for _, wantName := range tt.wantNames {
				found := false
				for _, p := range providers {
					if p == wantName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected provider %q not found in %v", wantName, providers)
				}
			}
		})
	}
}

func TestBifrostAccount_GetKeysForProvider(t *testing.T) {
	profiles := testProfiles()
	account := NewBifrostAccount(profiles, nil, nil)
	ctx := context.Background()

	t.Run("returns keys for anthropic", func(t *testing.T) {
		keys, err := account.GetKeysForProvider(ctx, bifrostSchemas.Anthropic)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(keys) != 1 {
			t.Fatalf("got %d keys, want 1", len(keys))
		}
		key := keys[0]
		if key.Name != "primary" {
			t.Errorf("key name = %q, want %q", key.Name, "primary")
		}
		// Value should be resolved (plain text in this case)
		if key.Value.GetValue() != "sk-ant-test-key" {
			t.Errorf("key value = %q, want %q", key.Value.GetValue(), "sk-ant-test-key")
		}
		// Models whitelist should include the configured models
		if !key.Models.IsAllowed("claude-sonnet-4-20250514") {
			t.Error("expected claude-sonnet-4-20250514 to be allowed")
		}
	})

	t.Run("returns error for unknown provider", func(t *testing.T) {
		_, err := account.GetKeysForProvider(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}

func TestBifrostAccount_GetKeysForProvider_VaultResolve(t *testing.T) {
	profiles := &config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				Keys: []config.KeyConfig{
					{
						Name:  "vault-key",
						Value: "vault://providers/anthropic/primary",
						Models: []config.ModelConfig{
							{Name: "claude-sonnet-4-20250514"},
						},
					},
				},
			},
		},
	}

	// Set up env vault backend with the key
	vs := vault.NewEnvVaultBackend()
	_ = vs.Set("providers/anthropic/primary", "sk-resolved-from-vault")
	defer func() { _ = vs.Delete("providers/anthropic/primary") }()

	account := NewBifrostAccount(profiles, vs, nil)
	ctx := context.Background()

	keys, err := account.GetKeysForProvider(ctx, bifrostSchemas.Anthropic)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}
	resolved := keys[0].Value.GetValue()
	if resolved != "sk-resolved-from-vault" {
		t.Errorf("vault-resolved value = %q, want %q", resolved, "sk-resolved-from-vault")
	}
}

func TestBifrostAccount_GetConfigForProvider(t *testing.T) {
	profiles := testProfiles()
	account := NewBifrostAccount(profiles, nil, nil)

	t.Run("returns config for anthropic", func(t *testing.T) {
		cfg, err := account.GetConfigForProvider(bifrostSchemas.Anthropic)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		// Default timeout should be set
		if cfg.NetworkConfig.DefaultRequestTimeoutInSeconds <= 0 {
			t.Error("expected positive default timeout")
		}
	})

	t.Run("returns error for unknown provider", func(t *testing.T) {
		_, err := account.GetConfigForProvider("nonexistent")
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}
