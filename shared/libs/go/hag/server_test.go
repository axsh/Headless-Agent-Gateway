package hag

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/llmgateway"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/vault"
)

func TestNew_DefaultConfig(t *testing.T) {
	srv, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv == nil {
		t.Fatal("New() returned nil server")
	}
	if srv.cfg == nil {
		t.Fatal("expected non-nil default config")
	}
	if srv.logger == nil {
		t.Fatal("expected non-nil default logger")
	}
	if srv.gateway == nil {
		t.Fatal("expected non-nil default gateway")
	}
}

func TestNew_WithConfig(t *testing.T) {
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port: 15000,
		},
	}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.cfg.LLMGateway.Port != 15000 {
		t.Errorf("cfg.LLMGateway.Port = %d, want 15000", srv.cfg.LLMGateway.Port)
	}
}

func TestNew_WithConfigPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
llm_gateway:
  port: 16000
vault:
  backend: "env"
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	srv, err := New(WithConfigPath(cfgPath))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.cfg.LLMGateway.Port != 16000 {
		t.Errorf("cfg.LLMGateway.Port = %d, want 16000", srv.cfg.LLMGateway.Port)
	}
}

func TestNew_WithLogger(t *testing.T) {
	customLog := logger.NewDefault(logger.LevelDebug)
	srv, err := New(WithLogger(customLog))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// The custom logger should be used (not replaced).
	if srv.logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNew_WithVaultStore(t *testing.T) {
	customVault := vault.NewEnvVaultBackend()
	srv, err := New(WithVaultStore(customVault))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.vault == nil {
		t.Fatal("expected non-nil vault")
	}
}

func TestNew_WithGateway(t *testing.T) {
	stub := llmgateway.NewStubGateway()
	srv, err := New(WithGateway(stub))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.Gateway() != stub {
		t.Error("Gateway() did not return the injected stub")
	}
}

func TestNew_OptionPriority(t *testing.T) {
	// WithConfig sets port=14000, then individual config override.
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port: 14000,
		},
	}
	// WithGateway overrides the auto-created gateway.
	stub := llmgateway.NewStubGateway()

	srv, err := New(WithConfig(cfg), WithGateway(stub))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if srv.cfg.LLMGateway.Port != 14000 {
		t.Errorf("cfg.LLMGateway.Port = %d, want 14000", srv.cfg.LLMGateway.Port)
	}
	if srv.Gateway() != stub {
		t.Error("WithGateway should override auto-created gateway")
	}
}

func TestServer_LaunchShutdown(t *testing.T) {
	stub := llmgateway.NewStubGateway()
	srv, err := New(WithGateway(stub))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !stub.Launched {
		t.Error("expected gateway.Launch() to be called")
	}

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !stub.ShutDown {
		t.Error("expected gateway.Shutdown() to be called")
	}
}

func TestServer_Gateway_ReturnsInjected(t *testing.T) {
	stub := llmgateway.NewStubGateway()
	srv, err := New(WithGateway(stub))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := srv.Gateway(); got != stub {
		t.Errorf("Gateway() returned %v, want injected stub", got)
	}
}

func TestNew_InvalidConfigPath(t *testing.T) {
	_, err := New(WithConfigPath("/nonexistent/config.yaml"))
	if err == nil {
		t.Fatal("expected error for invalid config path")
	}
}
