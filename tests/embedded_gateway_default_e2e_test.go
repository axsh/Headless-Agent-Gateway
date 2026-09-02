package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

// TestEmbeddedLaunch_CachesGatewayDefault verifies Issue #63 fix:
// server.Launch alone calls FetchModelsFromGateway, so Agent Service
// gatewayDefault matches LLMGP and omitted CreateSession model is filled.
//
// Unlike startE2EServer, this test intentionally does NOT call
// FetchModelsFromGateway after Launch (embedded library consumers get that path).
func TestEmbeddedLaunch_CachesGatewayDefault(t *testing.T) {
	modelProfilesSrc, _ := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
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
		t.Fatalf("write config: %v", err)
	}

	srv, err := server.New(server.WithConfigPath(tmpConfig))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Launch(context.Background()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	})

	// Control: LLMGP itself has default_model from profiles.
	gwURL := fmt.Sprintf("http://127.0.0.1:%d", gwPort)
	waitURLOK(t, gwURL+"/v1/models", 15*time.Second)
	gwResp, err := http.Get(gwURL + "/v1/models")
	if err != nil {
		t.Fatalf("GET gateway /v1/models: %v", err)
	}
	defer gwResp.Body.Close()
	var gwBody struct {
		Models       []llmgateway.ModelInfo `json:"models"`
		DefaultModel *llmgateway.ModelInfo  `json:"default_model"`
	}
	if err := json.NewDecoder(gwResp.Body).Decode(&gwBody); err != nil {
		t.Fatalf("decode gateway models: %v", err)
	}
	if gwBody.DefaultModel == nil || gwBody.DefaultModel.Model == "" {
		t.Fatalf("control failed: gateway default_model empty (profiles broken?) body=%+v", gwBody)
	}
	wantDefault := gwBody.DefaultModel.Model
	t.Logf("LLMGP default_model=%q n_models=%d", wantDefault, len(gwBody.Models))

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", srv.AgentService().Port())
	waitURLOK(t, baseURL+"/api/v1/models", 10*time.Second)

	asResp, err := http.Get(baseURL + "/api/v1/models")
	if err != nil {
		t.Fatalf("GET agent /api/v1/models: %v", err)
	}
	defer asResp.Body.Close()
	var asBody struct {
		Models       []llmgateway.ModelInfo `json:"models"`
		DefaultModel *llmgateway.ModelInfo  `json:"default_model"`
	}
	if err := json.NewDecoder(asResp.Body).Decode(&asBody); err != nil {
		t.Fatalf("decode agent models: %v", err)
	}

	// Expected: Agent Service mirrors LLMGP default after Launch (Issue #63).
	if asBody.DefaultModel == nil || asBody.DefaultModel.Model == "" {
		t.Errorf("Agent Service default_model empty after Launch (LLMGP had %q, n_models=%d)",
			wantDefault, len(asBody.Models))
	} else if asBody.DefaultModel.Model != wantDefault {
		t.Errorf("default_model = %q, want %q", asBody.DefaultModel.Model, wantDefault)
	}
	if len(asBody.Models) == 0 {
		t.Errorf("Agent Service models empty after Launch (LLMGP n_models=%d)", len(gwBody.Models))
	}

	workDir := t.TempDir()
	initGitRepo(t, workDir)
	sessionDir := filepath.Join(workDir, "sessions")
	_ = os.MkdirAll(sessionDir, 0755)
	createBody, _ := json.Marshal(map[string]string{
		"agent":       "claudecode",
		"work_dir":    workDir,
		"session_dir": sessionDir,
	})
	createResp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status=%d", createResp.StatusCode)
	}
	var created map[string]string
	_ = json.NewDecoder(createResp.Body).Decode(&created)
	sid := created["session_id"]
	if sid == "" {
		t.Fatal("empty session_id")
	}

	getResp, err := http.Get(baseURL + "/api/v1/sessions/" + sid)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer getResp.Body.Close()
	var sess struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(getResp.Body).Decode(&sess)
	if sess.Model == "" {
		t.Errorf("CreateSession without model left session.model empty (want gateway default %q)", wantDefault)
	} else if sess.Model != wantDefault {
		t.Errorf("session.model = %q, want %q", sess.Model, wantDefault)
	}
}

func waitURLOK(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s: %v", url, lastErr)
}
