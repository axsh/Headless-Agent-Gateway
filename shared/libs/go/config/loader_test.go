package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
llm_gateway:
  port: 14000
  model_profiles_path: "./model_profiles.yaml"
vault:
  backend: "env"
log:
  level: "info"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLMGateway.Port != 14000 {
		t.Errorf("Port = %d, want 14000", cfg.LLMGateway.Port)
	}
	if cfg.Vault.Backend != "env" {
		t.Errorf("Backend = %q, want %q", cfg.Vault.Backend, "env")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Level = %q, want %q", cfg.Log.Level, "info")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("{{{invalid yaml}}}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadModelProfiles_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model_profiles.yaml")
	content := `
default_profile:
  provider: "anthropic"
  model: "claude-sonnet-4-20250514"
providers:
  anthropic:
    api_keys:
      - name: "primary"
        secret: "vault://providers/anthropic/primary"
        models:
          - name: "claude-sonnet-4-20250514"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadModelProfiles(path)
	if err != nil {
		t.Fatalf("LoadModelProfiles() error = %v", err)
	}
	if cfg.DefaultProfile.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", cfg.DefaultProfile.Provider, "anthropic")
	}

	// Verify vault:// references are preserved as strings (not resolved).
	if cfg.Providers["anthropic"].ApiKeys[0].Secret != "vault://providers/anthropic/primary" {
		t.Errorf("key secret = %q, expected vault:// reference to be preserved", cfg.Providers["anthropic"].ApiKeys[0].Secret)
	}
}

func TestLoadModelProfiles_ValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model_profiles.yaml")
	// Empty providers should fail validation.
	content := `
default_profile:
  provider: "anthropic"
  model: "test"
providers: {}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadModelProfiles(path)
	if err == nil {
		t.Fatal("expected validation error for empty providers")
	}
}
