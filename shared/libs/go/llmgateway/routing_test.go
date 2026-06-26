package llmgateway

import (
	"errors"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

// testProfiles returns a ModelProfilesConfig with anthropic and openai providers.
func testProfiles() *config.ModelProfilesConfig {
	return &config.ModelProfilesConfig{
		Providers: map[string]config.ProviderConfig{
			"anthropic": {
				ApiKeys: []config.KeyConfig{
					{
						Name:  "primary",
						Secret: "sk-ant-test-key",
						Models: []config.ModelConfig{
							{Name: "claude-sonnet-4-20250514"},
							{Name: "claude-haiku-3-20240307"},
						},
					},
				},
			},
			"openai": {
				ApiKeys: []config.KeyConfig{
					{
						Name:  "default",
						Secret: "sk-openai-test-key",
						Models: []config.ModelConfig{
							{Name: "gpt-4o"},
							{Name: "gpt-4o-mini"},
							{Name: "gpt-5.3-codex", Mode: "responses"},
						},
					},
				},
			},
			"google": {
				ApiKeys: []config.KeyConfig{
					{
						Name:  "default",
						Secret: "AIzaSy-test-key",
						Models: []config.ModelConfig{
							{Name: "gemini-3.5-flash"},
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
			router := NewModelRouter(tt.profiles, nil, nil)
			got, err := router.ResolveModel(tt.modelName, "")

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

func TestModelRouter_SessionFallback(t *testing.T) {
	profiles := testProfiles()
	router := NewModelRouter(profiles, nil, nil)

	// 1. Resolve first model in session "session-1" -> should succeed and record it.
	got1, err := router.ResolveModel("claude-sonnet-4-20250514", "session-1")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}
	if got1.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got %q", got1.Model)
	}

	// 2. Resolve unknown model in session "session-1" -> should fallback to recorded default.
	got2, err := router.ResolveModel("unknown-model", "session-1")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if got2.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model to fallback to 'claude-sonnet-4-20250514', got %q", got2.Model)
	}

	// 3. Resolve unknown model in session "session-2" -> should fail because no default recorded.
	_, err = router.ResolveModel("unknown-model", "session-2")
	if err == nil {
		t.Fatalf("expected failure for unregistered session, got nil error")
	}
}

func TestModelRouter_ResolveModel_WithMode(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		wantMode  string
	}{
		{"empty mode for standard model", "gpt-4o", ""},
		{"responses mode for codex", "gpt-5.3-codex", "responses"},
		{"empty mode for anthropic", "claude-sonnet-4-20250514", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewModelRouter(testProfiles(), nil, nil)
			got, err := router.ResolveModel(tt.modelName, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Mode != tt.wantMode {
				t.Errorf("Mode = %q, want %q", got.Mode, tt.wantMode)
			}
		})
	}
}

func TestSessionMap_MaxSize(t *testing.T) {
	profiles := testProfiles()
	router := &ModelRouter{
		profiles:      profiles,
		sessionModels: make(map[string]*sessionEntry),
		maxSessions:   3,
		accessOrder:   make([]string, 0),
	}

	// Route 4 sessions
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-1")
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-2")
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-3")
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-4")

	// session-1 should be evicted (oldest)
	if _, exists := router.sessionModels["session-1"]; exists {
		t.Error("expected session-1 to be evicted, but it still exists")
	}

	// session-2, 3, 4 should exist
	for _, sid := range []string{"session-2", "session-3", "session-4"} {
		if _, exists := router.sessionModels[sid]; !exists {
			t.Errorf("expected %s to exist, but it was evicted", sid)
		}
	}
}

func TestSessionMap_TTL(t *testing.T) {
	profiles := testProfiles()
	router := &ModelRouter{
		profiles:      profiles,
		sessionModels: make(map[string]*sessionEntry),
		sessionTTL:    50 * time.Millisecond,
		accessOrder:   make([]string, 0),
	}

	// Route session-1
	_, err := router.ResolveModel("claude-sonnet-4-20250514", "session-1")
	if err != nil {
		t.Fatalf("ResolveModel failed: %v", err)
	}

	// Resolve unknown model within TTL -> should succeed due to fallback
	_, err = router.ResolveModel("unknown-model", "session-1")
	if err != nil {
		t.Errorf("expected fallback to succeed, got error: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Resolve unknown model after TTL -> should fail since it's expired
	_, err = router.ResolveModel("unknown-model", "session-1")
	if err == nil {
		t.Error("expected failure for expired session, got nil error")
	}
}

func TestSessionMap_LRU(t *testing.T) {
	profiles := testProfiles()
	router := &ModelRouter{
		profiles:      profiles,
		sessionModels: make(map[string]*sessionEntry),
		maxSessions:   3,
		accessOrder:   make([]string, 0),
	}

	// Route session-1, 2, 3
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-1")
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-2")
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-3")

	// Access session-1 to make it recently used
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-1")

	// Route session-4 (should trigger eviction)
	_, _ = router.ResolveModel("claude-sonnet-4-20250514", "session-4")

	// session-2 should be evicted (session-1 was touched, so session-2 is now the oldest)
	if _, exists := router.sessionModels["session-2"]; exists {
		t.Error("expected session-2 to be evicted, but it still exists")
	}

	// session-1, 3, 4 should exist
	for _, sid := range []string{"session-1", "session-3", "session-4"} {
		if _, exists := router.sessionModels[sid]; !exists {
			t.Errorf("expected %s to exist, but it was evicted", sid)
		}
	}
}
func TestModelRouter_ResolveModel_MaxOutputTokens(t *testing.T) {
	tests := []struct {
		name     string
		behavior *config.ModelBehavior
		want     int
	}{
		{
			name:     "max_output_tokens set",
			behavior: &config.ModelBehavior{MaxOutputTokens: 65536},
			want:     65536,
		},
		{
			name:     "max_output_tokens not set",
			behavior: &config.ModelBehavior{},
			want:     0,
		},
		{
			name:     "behavior nil",
			behavior: nil,
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profiles := &config.ModelProfilesConfig{
				Providers: map[string]config.ProviderConfig{
					"google": {
						ApiKeys: []config.KeyConfig{{
							Name:   "default",
							Secret: "test-key",
							Models: []config.ModelConfig{{
								Name:     "gemini-2.5-flash",
								Behavior: tt.behavior,
							}},
						}},
					},
				},
			}
			router := NewModelRouter(profiles, nil, nil)
			got, err := router.ResolveModel("gemini-2.5-flash", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.MaxOutputTokens != tt.want {
				t.Errorf("MaxOutputTokens = %d, want %d", got.MaxOutputTokens, tt.want)
			}
		})
	}
}
