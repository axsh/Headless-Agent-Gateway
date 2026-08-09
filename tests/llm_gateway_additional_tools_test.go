package llm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/vault"
	bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

const issue33MissingTypeMsg = "Missing required parameter: 'input[0].tools[0].type'"

func requireOpenAIKeyring(t *testing.T) {
	t.Helper()
	kb := vault.NewKeyringVaultBackend()
	if _, err := kb.Resolve("vault://providers/openai/default"); err != nil {
		t.Fatalf("openai API key required for live AdditionalTools test (not skippable): bin/vault-cli set --provider openai --stdin: %v", err)
	}
}

func additionalToolsLiveFixture(model string) map[string]any {
	return map[string]any{
		"model":  model,
		"stream": false,
		"tools":  []any{},
		"input": []any{
			map[string]any{
				"type": "additional_tools",
				"role": "developer",
				"tools": []any{
					map[string]any{"type": "custom", "name": "example_custom"},
					map[string]any{
						"type":        "function",
						"name":        "example_fn",
						"description": "example",
						"parameters": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"q": map[string]any{"type": "string"},
							},
							"required":             []any{"q"},
							"additionalProperties": false,
						},
					},
					map[string]any{
						"type":        "namespace",
						"name":        "example_ns",
						"description": "ns",
						"tools": []any{
							map[string]any{
								"type":        "function",
								"name":        "child",
								"description": "child fn",
								"parameters": map[string]any{
									"type":                 "object",
									"properties":           map[string]any{},
									"additionalProperties": false,
								},
							},
						},
					},
				},
			},
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": "Reply exactly with OK.",
			},
		},
	}
}

// TestLLMGateway_Responses_AdditionalTools_RoundTrip is a key-free regression
// for Issue #33 nested type preservation through Bifrost Responses conversion.
func TestLLMGateway_Responses_AdditionalTools_RoundTrip(t *testing.T) {
	bodyMap := additionalToolsLiveFixture("gpt-5.3-codex")
	body, err := json.Marshal(bodyMap)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	var oaiReq bifrostOpenAI.OpenAIResponsesRequest
	if err := json.Unmarshal(body, &oaiReq); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	bifrostCtx := bifrostSchemas.NewBifrostContext(context.Background(), bifrostSchemas.NoDeadline)
	bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)
	outReq := bifrostOpenAI.ToOpenAIResponsesRequest(bifrostCtx, bifrostReq)
	outBytes, err := outReq.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(outBytes, &wire); err != nil {
		t.Fatalf("wire unmarshal: %v\n%s", err, string(outBytes))
	}
	input, ok := wire["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("expected input array, got: %s", string(outBytes))
	}
	item0, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("input[0] type %T", input[0])
	}
	if item0["type"] != "additional_tools" {
		t.Fatalf("input[0].type = %v, want additional_tools", item0["type"])
	}
	tools, ok := item0["tools"].([]any)
	if !ok || len(tools) < 3 {
		t.Fatalf("input[0].tools short/missing (Issue #33); body=%s", string(outBytes))
	}
	wantTypes := []string{"custom", "function", "namespace"}
	for i, want := range wantTypes {
		tool := tools[i].(map[string]any)
		got, _ := tool["type"].(string)
		if got != want {
			t.Fatalf("input[0].tools[%d].type = %q, want %q (missing type reproduces Issue #33)", i, got, want)
		}
	}
}

// TestLLMGateway_Responses_AdditionalTools_LiveOpenAI posts the additional_tools
// fixture through Tern's /v1/responses to real OpenAI. Must not return Issue #33's
// missing input[0].tools[0].type upstream_error. Key absence is a hard Fail.
func TestLLMGateway_Responses_AdditionalTools_LiveOpenAI(t *testing.T) {
	requireOpenAIKeyring(t)

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	// gpt-5.3-codex is registered with mode: responses in testdata/model_profiles.yaml
	body := additionalToolsLiveFixture("gpt-5.3-codex")

	client := &http.Client{Timeout: 120 * time.Second}
	resp := postWithAuth(t, token, client, baseURL+"/v1/responses", body)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(respBody)
	t.Logf("LiveOpenAI status=%d body_prefix=%s", resp.StatusCode, truncateForLog(text, 500))

	if strings.Contains(text, issue33MissingTypeMsg) {
		t.Fatalf("Issue #33 regression: response contains %q (status=%d body=%s)",
			issue33MissingTypeMsg, resp.StatusCode, truncateForLog(text, 2000))
	}
	if strings.Contains(text, `"code":"upstream_error"`) && strings.Contains(text, "input[0].tools[0].type") {
		t.Fatalf("Issue #33 regression: upstream_error with tools type missing (status=%d body=%s)",
			resp.StatusCode, truncateForLog(text, 2000))
	}
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(text, "tools[0].type") {
		t.Fatalf("Issue #33-like 400 on tools type (status=%d body=%s)", resp.StatusCode, truncateForLog(text, 2000))
	}

	// Other upstream errors are out of scope for Issue #33 but worth logging.
	if resp.StatusCode >= 400 {
		t.Logf("non-Issue-33 error status=%d (allowed if not missing type): %s",
			resp.StatusCode, truncateForLog(text, 800))
	}
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
