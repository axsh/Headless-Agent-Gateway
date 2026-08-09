package openai_test

import (
	"context"
	"encoding/json"
	"testing"

	bifrostOpenAI "github.com/maximhq/bifrost/core/providers/openai"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

// additionalToolsFixtureJSON returns the Issue #33 / scenario C fixture body.
// Nested tools under input[0] (type=additional_tools) each carry a type discriminator.
func additionalToolsFixtureJSON(model string) []byte {
	payload := map[string]any{
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
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return b
}

// roundTripAdditionalTools mirrors Tern openai/handler.go:
// Unmarshal → ToBifrostResponsesRequest → ToOpenAIResponsesRequest → MarshalJSON.
func roundTripAdditionalTools(t *testing.T, body []byte) []byte {
	t.Helper()

	var oaiReq bifrostOpenAI.OpenAIResponsesRequest
	if err := json.Unmarshal(body, &oaiReq); err != nil {
		t.Fatalf("unmarshal OpenAIResponsesRequest: %v", err)
	}

	bifrostCtx := bifrostSchemas.NewBifrostContext(context.Background(), bifrostSchemas.NoDeadline)
	bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)
	if bifrostReq == nil {
		t.Fatal("ToBifrostResponsesRequest returned nil")
	}

	outReq := bifrostOpenAI.ToOpenAIResponsesRequest(bifrostReq)
	if outReq == nil {
		t.Fatal("ToOpenAIResponsesRequest returned nil")
	}

	outBytes, err := outReq.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	return outBytes
}

func parseWireInput(t *testing.T, outBytes []byte) []any {
	t.Helper()
	var wire map[string]any
	if err := json.Unmarshal(outBytes, &wire); err != nil {
		t.Fatalf("unmarshal wire JSON: %v\nbody=%s", err, string(outBytes))
	}
	input, ok := wire["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("expected non-empty input array, got: %s", string(outBytes))
	}
	return input
}

// TestResponsesAdditionalTools_RoundTripPreservesNestedType verifies nested
// tools[].type survive Bifrost round-trip (Issue #33 / Bifrost #5100).
// On Bifrost v1.5.18 this FAILs (types stripped via mcp_list_tools shape).
func TestResponsesAdditionalTools_RoundTripPreservesNestedType(t *testing.T) {
	body := additionalToolsFixtureJSON("gpt-5.3-codex")
	outBytes := roundTripAdditionalTools(t, body)
	input := parseWireInput(t, outBytes)

	item0, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("input[0] is not an object: %T", input[0])
	}
	if got := item0["type"]; got != "additional_tools" {
		t.Fatalf("input[0].type = %v, want additional_tools (Issue #33 round-trip)", got)
	}

	tools, ok := item0["tools"].([]any)
	if !ok || len(tools) < 3 {
		t.Fatalf("input[0].tools missing or short (len=%d); missing type reproduces Issue #33. body=%s",
			len(tools), string(outBytes))
	}

	wantTypes := []string{"custom", "function", "namespace"}
	for i, want := range wantTypes {
		tool, ok := tools[i].(map[string]any)
		if !ok {
			t.Fatalf("input[0].tools[%d] is not an object", i)
		}
		got, _ := tool["type"].(string)
		if got != want {
			t.Fatalf("input[0].tools[%d].type = %q, want %q (missing type reproduces Issue #33)", i, got, want)
		}
	}
}

// TestResponsesAdditionalTools_NamespaceChildTypePreserved checks namespace children.
func TestResponsesAdditionalTools_NamespaceChildTypePreserved(t *testing.T) {
	body := additionalToolsFixtureJSON("gpt-5.3-codex")
	outBytes := roundTripAdditionalTools(t, body)
	input := parseWireInput(t, outBytes)

	item0 := input[0].(map[string]any)
	tools := item0["tools"].([]any)
	if len(tools) < 3 {
		t.Fatalf("expected >=3 tools, got %d; body=%s", len(tools), string(outBytes))
	}
	ns, ok := tools[2].(map[string]any)
	if !ok {
		t.Fatal("namespace tool is not an object")
	}
	if ns["type"] != "namespace" {
		t.Fatalf("tools[2].type = %v, want namespace", ns["type"])
	}
	children, ok := ns["tools"].([]any)
	if !ok || len(children) == 0 {
		t.Fatalf("namespace.tools missing; body=%s", string(outBytes))
	}
	child := children[0].(map[string]any)
	if child["type"] != "function" {
		t.Fatalf("namespace.tools[0].type = %v, want function (Issue #33)", child["type"])
	}
	if child["name"] != "child" {
		t.Fatalf("namespace.tools[0].name = %v, want child", child["name"])
	}
}

// TestResponsesAdditionalTools_TopLevelToolsRemainEmpty ensures tools:[] is preserved.
func TestResponsesAdditionalTools_TopLevelToolsRemainEmpty(t *testing.T) {
	body := additionalToolsFixtureJSON("gpt-5.3-codex")
	outBytes := roundTripAdditionalTools(t, body)

	var wire map[string]any
	if err := json.Unmarshal(outBytes, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tools, ok := wire["tools"]; ok {
		arr, isArr := tools.([]any)
		if !isArr {
			t.Fatalf("top-level tools is %T, want array", tools)
		}
		if len(arr) != 0 {
			t.Fatalf("top-level tools len=%d, want 0 (Codex code-mode sends tools:[])", len(arr))
		}
	}
}
