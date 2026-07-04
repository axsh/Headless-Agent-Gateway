package llm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

// getFreePort listens on an ephemeral port and returns it.
func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// ensureGoogleAPIKey checks that the Google API key is configured in the keyring.
func ensureGoogleAPIKey(t *testing.T) {
	t.Helper()
	vs := vault.NewKeyringVaultBackend()
	apiKey, err := vs.Resolve("vault://providers/google/default")
	if err != nil || apiKey == "" {
		t.Fatalf("no google api key found in keyring. Please set it using: ./bin/vault-cli set --provider google --key default [key]")
	}
}

func TestGeminiE2E_NonStream(t *testing.T) {
	ensureGoogleAPIKey(t)

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              0, // auto-assign
			ModelProfilesPath: profilesPath,
		},
		Vault: config.VaultConfig{
			Backends: []string{"keyring"},
		},
		AgentService: config.AgentServiceConfig{
			Port: getFreePort(t), // dynamic free port
		},
		WebSocket: config.WebSocketConfig{
			Port: getFreePort(t), // dynamic free port
		},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())
	baseURL := srv.Gateway().ProxyURL()
	token := srv.GatewayToken()

	body := map[string]any{
		"model":      "gemini-2.5-flash",
		"max_tokens": 100,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello Gemini, reply only with 'Hello'"},
		},
	}

	resp := postWithAuth(t, token, nil, baseURL+"/v1/messages", body)
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("JSON decode failed: %v\nbody: %s", err, string(respBody))
	}

	// Response must be in Anthropic format
	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
	if result["role"] != "assistant" {
		t.Errorf("expected role=assistant, got %v", result["role"])
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got: %s", string(respBody))
	}
	block := content[0].(map[string]any)
	if block["type"] != "text" || !strings.Contains(strings.ToLower(block["text"].(string)), "hello") {
		t.Errorf("unexpected content block: %v", block)
	}

	if result["stop_reason"] != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %v", result["stop_reason"])
	}
}

func TestGeminiE2E_Stream(t *testing.T) {
	ensureGoogleAPIKey(t)

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              0, // auto-assign
			ModelProfilesPath: profilesPath,
		},
		Vault: config.VaultConfig{
			Backends: []string{"keyring"},
		},
		AgentService: config.AgentServiceConfig{
			Port: getFreePort(t), // dynamic free port
		},
		WebSocket: config.WebSocketConfig{
			Port: getFreePort(t), // dynamic free port
		},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())
	baseURL := srv.Gateway().ProxyURL()
	token := srv.GatewayToken()

	body := map[string]any{
		"model":      "gemini-2.5-flash",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello Gemini, reply only with 'Hello'"},
		},
	}

	resp := postWithAuth(t, token, nil, baseURL+"/v1/messages", body)
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	events := string(respBody)

	// Verify Anthropic SSE event format
	if !strings.Contains(events, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(events, "event: content_block_start") {
		t.Error("missing content_block_start event")
	}
	if !strings.Contains(events, "event: content_block_delta") {
		t.Error("missing content_block_delta event")
	}
	if !strings.Contains(events, "event: message_stop") {
		t.Error("missing message_stop event")
	}
}

func TestGeminiE2E_CawaClient_FileCreation(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("skipping E2E test; claude CLI not found in PATH")
	}
	ensureGoogleAPIKey(t)

	// Set config
	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	// Discover free ports
	gwPort := getFreePort(t)
	wsPort := getFreePort(t)
	asPort := getFreePort(t)

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              gwPort,
			ModelProfilesPath: profilesPath,
		},
		Vault: config.VaultConfig{
			Backends: []string{"keyring"},
		},
		AgentService: config.AgentServiceConfig{
			Port: asPort,
		},
		WebSocket: config.WebSocketConfig{
			Port: wsPort,
		},
	}

	// Launch tern
	srv, err := server.New(
		server.WithConfig(cfg),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	// Register claudecode agent with gateway URL
	gwURL := srv.Gateway().ProxyURL()
	adapter := claudecode.New(&codingagent.AdapterConfig{
		GatewayURL:   gwURL,
		DefaultModel: "gemini-2.5-flash",
	})
	srv.AgentService().RegisterAgent(adapter)

	// Ensure ternctl binary is built
	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("project root path: %v", err)
	}

	ternctlBin := "ternctl"
	if os.PathSeparator == '\\' {
		ternctlBin = "ternctl.exe"
	}
	ternctlPath := filepath.Join(projectRoot, "bin", ternctlBin)

	ternctlDir := filepath.Join(projectRoot, "features", "ternctl")
	buildCmd := exec.Command("go", "build", "-o", filepath.Join("..", "..", "bin", ternctlBin), ".")
	buildCmd.Dir = ternctlDir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build ternctl: %v\noutput: %s", err, string(output))
	}

	// Execute ternctl command
	workDir := t.TempDir()
	serverURL := fmt.Sprintf("http://localhost:%d", asPort)

	cmd := exec.Command(ternctlPath, 
		"--server", serverURL,
		"--log-level", "trace",
		"run",
		"--agent", "claudecode",
		"--model", "gemini-2.5-flash",
		"--prompt", "Create a file named test.txt containing exactly the text 'Hello Gemini E2E'. Do nothing else.",
		"--work-dir", workDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("running ternctl command: %s", cmd.String())
	err = cmd.Run()
	t.Logf("ternctl stdout:\n%s", stdout.String())
	t.Logf("ternctl stderr:\n%s", stderr.String())

	if err != nil {
		t.Fatalf("ternctl command failed: %v", err)
	}

	// Verify the file was created
	filePath := filepath.Join(workDir, "test.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("expected test.txt to be created in %s: %v", workDir, err)
	}

	if !strings.Contains(string(content), "Hello Gemini E2E") {
		t.Errorf("test.txt content = %q, want it to contain 'Hello Gemini E2E'", string(content))
	}
}
