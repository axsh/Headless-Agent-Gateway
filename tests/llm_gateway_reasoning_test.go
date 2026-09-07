package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	client "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestLLMGateway_GPT6Astra_APIExposure(t *testing.T) {
	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	gwPort := freePort(t)
	asPort := freePort(t)
	wsPort := freePort(t)

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              gwPort,
			ModelProfilesPath: profilesPath,
		},
		AgentService: config.AgentServiceConfig{
			Port: asPort,
		},
		WebSocket: config.WebSocketConfig{
			Port: wsPort,
		},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
		server.WithKeyringVault(),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	// 1. Verify GET /v1/models directly on Gateway
	gwURL := srv.Gateway().ProxyURL()
	resp, err := http.Get(gwURL + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/models status = %d, want 200", resp.StatusCode)
	}

	var gwBody struct {
		Models []struct {
			Provider  string                 `json:"provider"`
			Model     string                 `json:"model"`
			Reasoning *config.ModelReasoning `json:"reasoning,omitempty"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gwBody); err != nil {
		t.Fatalf("decode /v1/models: %v", err)
	}

	var astraModel *config.ModelReasoning
	for _, m := range gwBody.Models {
		if m.Model == "gpt-6-astra" {
			astraModel = m.Reasoning
			break
		}
	}
	if astraModel == nil {
		t.Fatal("gpt-6-astra not found in GET /v1/models or reasoning is nil")
	}
	if !astraModel.Required {
		t.Error("expected gpt-6-astra reasoning.required = true")
	}
	if astraModel.DefaultEffort != "medium" {
		t.Errorf("expected default_effort = medium, got %q", astraModel.DefaultEffort)
	}
	wantEfforts := []string{"minimal", "low", "medium", "high", "xhigh", "max"}
	if !reflect.DeepEqual(astraModel.SupportedEfforts, wantEfforts) {
		t.Errorf("supported_efforts = %v, want %v", astraModel.SupportedEfforts, wantEfforts)
	}

	// 2. Verify GET /api/v1/models on AgentService via client.ListModels
	asURL := fmt.Sprintf("http://127.0.0.1:%d", srv.AgentService().Port())
	c := client.New(asURL)
	cResp, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("client.ListModels: %v", err)
	}

	var clientAstra *client.ModelReasoning
	for _, m := range cResp.Models {
		if m.Model == "gpt-6-astra" {
			clientAstra = m.Reasoning
			break
		}
	}
	if clientAstra == nil {
		t.Fatal("gpt-6-astra not found in client.ListModels or reasoning is nil")
	}
	if !clientAstra.Required {
		t.Error("expected client reasoning.required = true")
	}
	if clientAstra.DefaultEffort != "medium" {
		t.Errorf("expected client default_effort = medium, got %q", clientAstra.DefaultEffort)
	}
	if !reflect.DeepEqual(clientAstra.SupportedEfforts, wantEfforts) {
		t.Errorf("client supported_efforts = %v, want %v", clientAstra.SupportedEfforts, wantEfforts)
	}
}

func TestLLMGateway_GPT6Astra_EarlyValidation(t *testing.T) {
	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	gwPort := freePort(t)
	asPort := freePort(t)
	wsPort := freePort(t)

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              gwPort,
			ModelProfilesPath: profilesPath,
		},
		AgentService: config.AgentServiceConfig{
			Port: asPort,
		},
		WebSocket: config.WebSocketConfig{
			Port: wsPort,
		},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
		server.WithKeyringVault(),
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

	tests := []struct {
		name        string
		effort      string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "effort none rejected",
			effort:      "none",
			wantCode:    "unsupported_reasoning_effort",
			wantMessage: "not supported",
		},
		{
			name:        "unknown effort rejected",
			effort:      "ultra-deep",
			wantCode:    "unsupported_reasoning_effort",
			wantMessage: "not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"model": "gpt-6-astra",
				"reasoning": map[string]any{
					"effort": tt.effort,
				},
				"input": []any{
					map[string]any{
						"type":    "message",
						"role":    "user",
						"content": "hello",
					},
				},
			}
			b, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", bytes.NewReader(b))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Gateway-Token", token)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 400; body: %s", resp.StatusCode, string(bodyBytes))
			}

			var errBody struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
					Code    string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if errBody.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", errBody.Error.Code, tt.wantCode)
			}
			if !strings.Contains(errBody.Error.Message, tt.wantMessage) {
				t.Errorf("error message %q does not contain %q", errBody.Error.Message, tt.wantMessage)
			}
		})
	}
}

func TestLLMGateway_GPT6Astra_BifrostConversion(t *testing.T) {
	// Directly verify that Bifrost's OpenAIResponsesRequest conversion
	// identifies gpt-6-astra as a reasoning model and preserves max and xhigh.
	bifrostCtx := bifrostSchemas.NewBifrostContext(context.Background(), bifrostSchemas.NoDeadline)

	tests := []struct {
		inputEffort string
		wantEffort  string
	}{
		{"max", "max"},
		{"xhigh", "xhigh"},
		{"high", "high"},
		{"medium", "medium"},
		{"low", "low"},
		{"minimal", "low"}, // Bifrost maps minimal to low for gpt-6-astra
	}

	for _, tt := range tests {
		t.Run("effort_"+tt.inputEffort, func(t *testing.T) {
			eff := tt.inputEffort
			oaiReq := bifrostOpenAI.OpenAIResponsesRequest{
				Model: "gpt-6-astra",
				ResponsesParameters: bifrostSchemas.ResponsesParameters{
					Reasoning: &bifrostSchemas.ResponsesParametersReasoning{
						Effort: &eff,
					},
				},
			}

			bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)
			if bifrostReq.Params == nil || bifrostReq.Params.Reasoning == nil {
				t.Fatalf("bifrostReq.Params.Reasoning is nil for effort %s", tt.inputEffort)
			}
			if *bifrostReq.Params.Reasoning.Effort != tt.inputEffort {
				t.Errorf("bifrostReq Effort = %q, want %q", *bifrostReq.Params.Reasoning.Effort, tt.inputEffort)
			}

			backReq := bifrostOpenAI.ToOpenAIResponsesRequest(bifrostCtx, bifrostReq)
			if backReq.Reasoning == nil || backReq.Reasoning.Effort == nil {
				t.Fatalf("backReq.Reasoning.Effort is nil for effort %s", tt.inputEffort)
			}
			if *backReq.Reasoning.Effort != tt.wantEffort {
				t.Errorf("backReq Effort = %q, want %q (not downgraded)", *backReq.Reasoning.Effort, tt.wantEffort)
			}

			jsonBytes, err := backReq.MarshalJSON()
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}

			var wire map[string]any
			if err := json.Unmarshal(jsonBytes, &wire); err != nil {
				t.Fatalf("wire unmarshal: %v", err)
			}
			rObj, ok := wire["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("expected wire reasoning object, got: %s", string(jsonBytes))
			}
			if rObj["effort"] != tt.wantEffort {
				t.Errorf("wire reasoning.effort = %v, want %s (patch verified)", rObj["effort"], tt.wantEffort)
			}
		})
	}
}

func TestLLMGateway_GPT6Astra_UpstreamTransparency(t *testing.T) {
	var mu sync.Mutex
	var capturedBodies [][]byte

	// 1. Mock Upstream OpenAI Responses API server
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		mu.Lock()
		capturedBodies = append(capturedBodies, body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "resp_mock_123",
			"object": "response",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": "ok from mock"
				}
			]
		}`))
	}))
	defer mockUpstream.Close()

	// 2. Setup isolated model_profiles.yaml pointing to mockUpstream
	tmpDir := t.TempDir()
	profilesFile := filepath.Join(tmpDir, "model_profiles.yaml")
	profilesYAML := fmt.Sprintf(`
default_profile:
  provider: openai
  model: gpt-6-astra

providers:
  openai:
    api_keys:
      - name: default
        secret: "mock-api-key"
        models:
          - name: gpt-6-astra
            mode: responses
            behavior:
              reasoning:
                required: true
                supported_efforts:
                  - minimal
                  - low
                  - medium
                  - high
                  - xhigh
                  - max
                default_effort: medium
    network_config:
      base_url: "%s"
`, mockUpstream.URL)

	if err := os.WriteFile(profilesFile, []byte(profilesYAML), 0644); err != nil {
		t.Fatalf("write profiles: %v", err)
	}

	gwPort := freePort(t)
	asPort := freePort(t)
	wsPort := freePort(t)

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              gwPort,
			ModelProfilesPath: profilesFile,
		},
		AgentService: config.AgentServiceConfig{
			Port: asPort,
		},
		WebSocket: config.WebSocketConfig{
			Port: wsPort,
		},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
		server.WithKeyringVault(),
	)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	token := srv.GatewayToken()
	gwURL := srv.Gateway().ProxyURL()

	sendRequest := func(t *testing.T, payload map[string]any) {
		t.Helper()
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, gwURL+"/v1/responses", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gateway-Token", token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, string(body))
		}
	}

	// Case A: Omit reasoning parameter -> Expect default "medium" backfilled and sent to upstream
	sendRequest(t, map[string]any{
		"model": "gpt-6-astra",
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "case A",
			},
		},
	})

	// Case B: Explicit "low" effort -> Expect "low" sent to upstream
	sendRequest(t, map[string]any{
		"model": "gpt-6-astra",
		"reasoning": map[string]any{
			"effort": "low",
		},
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "case B",
			},
		},
	})

	// Case C: Explicit "max" effort -> Expect "max" preserved (not stripped or downgraded to high)
	sendRequest(t, map[string]any{
		"model": "gpt-6-astra",
		"reasoning": map[string]any{
			"effort": "max",
		},
		"input": []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "case C",
			},
		},
	})

	mu.Lock()
	defer mu.Unlock()

	if len(capturedBodies) != 3 {
		t.Fatalf("expected 3 captured requests at upstream, got %d", len(capturedBodies))
	}

	// Verify Case A: backfilled to "medium"
	var wireA map[string]any
	if err := json.Unmarshal(capturedBodies[0], &wireA); err != nil {
		t.Fatalf("unmarshal case A wire: %v", err)
	}
	rA, ok := wireA["reasoning"].(map[string]any)
	if !ok || rA["effort"] != "medium" {
		t.Errorf("Case A wire reasoning = %v, want effort: medium (body: %s)", wireA["reasoning"], string(capturedBodies[0]))
	}

	// Verify Case B: preserved as "low"
	var wireB map[string]any
	if err := json.Unmarshal(capturedBodies[1], &wireB); err != nil {
		t.Fatalf("unmarshal case B wire: %v", err)
	}
	rB, ok := wireB["reasoning"].(map[string]any)
	if !ok || rB["effort"] != "low" {
		t.Errorf("Case B wire reasoning = %v, want effort: low (body: %s)", wireB["reasoning"], string(capturedBodies[1]))
	}

	// Verify Case C: preserved as "max" (Bifrost patch verification)
	var wireC map[string]any
	if err := json.Unmarshal(capturedBodies[2], &wireC); err != nil {
		t.Fatalf("unmarshal case C wire: %v", err)
	}
	rC, ok := wireC["reasoning"].(map[string]any)
	if !ok || rC["effort"] != "max" {
		t.Errorf("Case C wire reasoning = %v, want effort: max (body: %s)", wireC["reasoning"], string(capturedBodies[2]))
	}
}

func TestLLMGateway_GPT6Astra_LiveUpstream(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	gwPort := freePort(t)
	asPort := freePort(t)
	wsPort := freePort(t)

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              gwPort,
			ModelProfilesPath: profilesPath,
		},
		AgentService: config.AgentServiceConfig{
			Port: asPort,
		},
		WebSocket: config.WebSocketConfig{
			Port: wsPort,
		},
	}

	srv, err := server.New(
		server.WithConfig(cfg),
		server.WithKeyringVault(),
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

	client := &http.Client{Timeout: 90 * time.Second}

	cases := []struct {
		name       string
		effort     string
		wantEffort string
	}{
		{
			name:       "default backfill (medium)",
			effort:     "",
			wantEffort: "medium",
		},
		{
			name:       "explicit low effort",
			effort:     "low",
			wantEffort: "low",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{
				"model": "gpt-6-astra",
				"input": []any{
					map[string]any{
						"type":    "message",
						"role":    "user",
						"content": "Reply with 'ok' only.",
					},
				},
			}
			if tc.effort != "" {
				payload["reasoning"] = map[string]any{
					"effort": tc.effort,
				}
			}

			bodyBytes, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", bytes.NewReader(bodyBytes))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Gateway-Token", token)

			t.Logf("[%s] Sending live request to OpenAI for model gpt-6-astra...", tc.name)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
			}

			var res struct {
				Model     string `json:"model"`
				Reasoning struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
				Output []struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			}
			if err := json.Unmarshal(respBody, &res); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			t.Logf("[%s] Model=%s, Reasoning.Effort=%s, Output=%+v", tc.name, res.Model, res.Reasoning.Effort, res.Output)
			if res.Model != "gpt-6-astra" {
				t.Errorf("model = %q, want gpt-6-astra", res.Model)
			}
			if res.Reasoning.Effort != tc.wantEffort {
				t.Errorf("reasoning.effort = %q, want %q", res.Reasoning.Effort, tc.wantEffort)
			}
		})
	}
}

