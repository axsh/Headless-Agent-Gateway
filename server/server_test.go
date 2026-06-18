package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
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

// TC-P2-01: WithConfigPath overrides WithConfig.
func TestNew_ConfigPathOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
llm_gateway:
  port: 17000
vault:
  backend: "env"
`)
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfgDirect := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port: 18000,
		},
	}

	stub := llmgateway.NewStubGateway()
	srv, err := New(WithConfigPath(cfgPath), WithConfig(cfgDirect), WithGateway(stub))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// WithConfigPath should take priority over WithConfig.
	if srv.cfg.LLMGateway.Port != 17000 {
		t.Errorf("cfg.LLMGateway.Port = %d, want 17000 (from WithConfigPath)", srv.cfg.LLMGateway.Port)
	}
}

// TC-P2-02: Gateway Launch failure propagates through Server.Launch().
func TestServer_Launch_GatewayError(t *testing.T) {
	failing := &failingGateway{err: fmt.Errorf("port in use")}
	srv, err := New(WithGateway(failing))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = srv.Launch(context.Background())
	if err == nil {
		t.Fatal("expected Launch() to return error")
	}
	if !strings.Contains(err.Error(), "gateway launch") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "gateway launch")
	}
	if !strings.Contains(err.Error(), "port in use") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "port in use")
	}
}

// failingGateway is a test double that fails on Launch.
type failingGateway struct {
	llmgateway.StubGateway
	err error
}

func (f *failingGateway) Launch(_ context.Context) error {
	return f.err
}

// TC-P2-07: server.Server end-to-end lifecycle with real ProxyServer.
func TestServer_EndToEnd_WithProxyServer(t *testing.T) {
	// Use port=0 for ephemeral port. No WithGateway -> auto-creates ProxyServer.
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port: 0,
		},
	}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	// Verify the gateway is a real ProxyServer and reachable.
	url := srv.Gateway().ProxyURL()
	if url == "" {
		t.Fatal("ProxyURL() returned empty string")
	}

	resp, err := http.Get(url + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var health llmgateway.HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("health.status = %q, want %q", health.Status, "ok")
	}

	// Shutdown and verify unreachable.
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	_, err = http.Get(url + "/health")
	if err == nil {
		t.Fatal("expected error after Shutdown(), got nil")
	}
}

func TestServer_ReloadModelProfiles_And_Skeletons(t *testing.T) {
	// Create a temp file for model_profiles.yaml
	dir := t.TempDir()
	profilesPath := filepath.Join(dir, "model_profiles.yaml")
	content := []byte(`
providers:
  openai:
    keys:
      - name: default
        value: sk-openai-test-key
        models:
          - name: gpt-4o
`)
	if err := os.WriteFile(profilesPath, content, 0644); err != nil {
		t.Fatalf("write profiles: %v", err)
	}

	// Create server
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              0,
			ModelProfilesPath: profilesPath,
		},
	}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify skeletons
	if srv.AgentService() == nil {
		t.Error("expected non-nil AgentService")
	}

	// Test reload
	err = srv.ReloadModelProfiles(profilesPath)
	if err != nil {
		t.Fatalf("ReloadModelProfiles failed: %v", err)
	}

	// Verify loaded models
	models := srv.Gateway().ListModels()
	if len(models) != 1 || models[0].Model != "gpt-4o" {
		t.Errorf("expected 1 model 'gpt-4o', got %v", models)
	}
}

func TestServer_TaskLog(t *testing.T) {
	stub := llmgateway.NewStubGateway()
	srv, err := New(WithGateway(stub))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tl := srv.TaskLog()
	if tl == nil {
		t.Fatal("TaskLog() returned nil")
	}

	// TaskLog should be functional.
	entry := tasklog.NewAgentLogEntry("test-agent", tasklog.WithKind("text"))
	tl.Add(entry)
	entries := tl.Entries()
	if len(entries) != 1 {
		t.Errorf("Entries() len = %d, want 1", len(entries))
	}
}

func TestServer_WebSocketURL(t *testing.T) {
	stub := llmgateway.NewStubGateway()
	cfg := &config.AppConfig{
		WebSocket: config.WebSocketConfig{Port: 0},
	}
	srv, err := New(WithGateway(stub), WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Before Launch, URL should be empty (port is 0).
	if url := srv.WebSocketURL(); url != "" {
		t.Errorf("before Launch: WebSocketURL() = %q, want empty", url)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer srv.Shutdown(ctx)

	url := srv.WebSocketURL()
	if url == "" {
		t.Fatal("after Launch: WebSocketURL() returned empty string")
	}
	if !strings.HasPrefix(url, "ws://") {
		t.Errorf("WebSocketURL() = %q, want ws:// prefix", url)
	}
}

func TestServer_AutoGenerateToken(t *testing.T) {
	cfg := &config.AppConfig{}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	token := srv.GatewayToken()
	if len(token) != 64 { // 32 bytes hex = 64 characters
		t.Errorf("expected 64-char token, got %q (len=%d)", token, len(token))
	}
}

func TestServer_StaticToken(t *testing.T) {
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			AuthToken: "my-static-token-abc-123",
		},
	}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	token := srv.GatewayToken()
	if token != "my-static-token-abc-123" {
		t.Errorf("GatewayToken() = %q, want %q", token, "my-static-token-abc-123")
	}
}

func TestServer_TLS_AutoMode(t *testing.T) {
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port: 0,
			TLS: config.TLSConfig{
				Enabled: true,
				Mode:    "auto",
			},
		},
	}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// ProxyURL should start with https
	url := srv.Gateway().ProxyURL()
	if !strings.HasPrefix(url, "https://") {
		t.Errorf("ProxyURL() = %q, want https:// prefix", url)
	}

	// CA cert path should be resolved and file should exist
	caPath := srv.TLSCACertPath()
	if caPath == "" {
		t.Error("expected non-empty TLSCACertPath")
	}
	if _, err := os.Stat(caPath); os.IsNotExist(err) {
		t.Errorf("CA cert file does not exist: %s", caPath)
	}
}

func TestServer_TLS_Disabled(t *testing.T) {
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port: 0,
			TLS: config.TLSConfig{
				Enabled: false,
			},
		},
	}
	srv, err := New(WithConfig(cfg))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	url := srv.Gateway().ProxyURL()
	if !strings.HasPrefix(url, "http://") {
		t.Errorf("ProxyURL() = %q, want http:// prefix", url)
	}

	if path := srv.TLSCACertPath(); path != "" {
		t.Errorf("expected empty TLSCACertPath when TLS disabled, got %q", path)
	}
}

// Test resolveAgentService with nil gateway (no agents registered since
// CLIs are not on PATH in test environment, but the function should not panic).
func TestResolveAgentService_NilGateway(t *testing.T) {
	o := &options{}
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()

	as := resolveAgentService(o, log, tl, "http://localhost:1234", "test-token", "", nil, false)
	if as == nil {
		t.Fatal("resolveAgentService returned nil")
	}
}

// Test resolveAgentService with a stub gateway that provides DefaultModel.
// Verifies the function does not panic and returns a valid server.
func TestResolveAgentService_WithStubGateway(t *testing.T) {
	o := &options{}
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	stub := llmgateway.NewStubGateway()

	as := resolveAgentService(o, log, tl, "http://localhost:1234", "test-token", "", stub, false)
	if as == nil {
		t.Fatal("resolveAgentService returned nil")
	}
}

// Test resolveAgentService with externally provided AgentService (bypass auto-registration).
func TestResolveAgentService_ExternalBypass(t *testing.T) {
	externalAS := agentservice.New()
	o := &options{agentService: externalAS}
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	stub := llmgateway.NewStubGateway()

	// When o.agentService is set, resolveAgentService should return it directly.
	got := resolveAgentService(o, log, tl, "http://localhost:1234", "test-token", "", stub, false)
	if got != externalAS {
		t.Error("expected externally provided AgentService to be returned directly")
	}
}
