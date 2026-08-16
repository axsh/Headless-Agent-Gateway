package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
)

func startGatewayE2E(t *testing.T, log *captureLogger) (gwURL, token string, cleanup func()) {
	t.Helper()

	modelProfilesSrc, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("model_profiles path: %v", err)
	}

	gwPort := freePort(t)
	wsPort := freePort(t)
	asPort := freePort(t)

	tmpDir := t.TempDir()
	tmpConfig := filepath.Join(tmpDir, "config.yaml")
	configContent := fmt.Sprintf(`llm_gateway:
  port: %d
  model_profiles_path: "%s"
log:
  level: "info"
vault:
  backends: [keyring]
websocket:
  port: %d
agent_service:
  port: %d
  disable_sandbox: true
`, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)
	if err := os.WriteFile(tmpConfig, []byte(configContent), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	opts := []server.Option{server.WithConfigPath(tmpConfig)}
	if log != nil {
		opts = append(opts, server.WithLogger(log))
	}
	srv, err := server.New(opts...)
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
	}
	if err := srv.Launch(context.Background()); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	cleanup = func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}
	return srv.Gateway().ProxyURL(), srv.GatewayToken(), cleanup
}

func TestGatewayErrorResponseLog(t *testing.T) {
	log := &captureLogger{}
	gwURL, token, cleanup := startGatewayE2E(t, log)
	defer cleanup()

	reqBody := bytes.NewBufferString(`{"model":"no-such-e2e-model"}`)
	req, err := http.NewRequest(http.MethodPost, gwURL+"/v1/responses", reqBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Gateway-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/responses: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", resp.StatusCode, raw)
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, raw)
	}
	if body.Error.Code != "model_not_found" {
		t.Fatalf("code = %q, want model_not_found", body.Error.Code)
	}
	if body.Error.Message != "model not found: no-such-e2e-model" {
		t.Fatalf("message = %q", body.Error.Message)
	}

	hit, ok := log.find("error", handlerctx.LogLLMGatewayErrorResponse)
	if !ok {
		t.Fatalf("missing ERROR %q", handlerctx.LogLLMGatewayErrorResponse)
	}
	if kvFmt(hit.kv, "code") != "model_not_found" {
		t.Errorf("log code = %s", kvFmt(hit.kv, "code"))
	}
	if kvFmt(hit.kv, "status") != "404" {
		t.Errorf("log status = %s", kvFmt(hit.kv, "status"))
	}
	if _, found := log.find("info", "openai responses request via bifrost"); found {
		t.Fatal("via bifrost Info must not be logged on 404")
	}
}
