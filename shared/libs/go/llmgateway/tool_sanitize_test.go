package llmgateway

import (
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func ptrStr(s string) *string { return &s }

func sampleToolsWithNamespace() []bifrostSchemas.ResponsesTool {
	return []bifrostSchemas.ResponsesTool{
		{
			Type: bifrostSchemas.ResponsesToolTypeFunction,
			Name: ptrStr("fn"),
			ResponsesToolFunction: &bifrostSchemas.ResponsesToolFunction{
				Parameters: &bifrostSchemas.ToolFunctionParameters{Type: "object"},
			},
		},
		{
			Type: bifrostSchemas.ResponsesToolTypeNamespace,
			Name: ptrStr("ns"),
			ResponsesToolNamespace: &bifrostSchemas.ResponsesToolNamespace{
				Tools: []bifrostSchemas.ResponsesTool{
					{Type: bifrostSchemas.ResponsesToolTypeFunction, Name: ptrStr("child")},
				},
			},
		},
		{
			Type:                   bifrostSchemas.ResponsesToolTypeWebSearch,
			ResponsesToolWebSearch: &bifrostSchemas.ResponsesToolWebSearch{},
		},
	}
}

// TestSanitizeToolsForProvider_OpenAI_NoOp locks R4: OpenAI path must not mutate tools.
func TestSanitizeToolsForProvider_OpenAI_NoOp(t *testing.T) {
	log := logger.NewDefault(logger.LevelError)
	tools := sampleToolsWithNamespace()
	req := &bifrostSchemas.BifrostResponsesRequest{
		Params: &bifrostSchemas.ResponsesParameters{Tools: tools},
	}

	SanitizeToolsForProvider(req, bifrostSchemas.OpenAI, log)

	if len(req.Params.Tools) != 3 {
		t.Fatalf("OpenAI sanitize changed tool count: got %d want 3", len(req.Params.Tools))
	}
	if req.Params.Tools[0].Type != bifrostSchemas.ResponsesToolTypeFunction {
		t.Errorf("tools[0].Type = %v, want function", req.Params.Tools[0].Type)
	}
	if req.Params.Tools[1].Type != bifrostSchemas.ResponsesToolTypeNamespace {
		t.Errorf("tools[1].Type = %v, want namespace (must not filter on OpenAI)", req.Params.Tools[1].Type)
	}
	if req.Params.Tools[2].Type != bifrostSchemas.ResponsesToolTypeWebSearch {
		t.Errorf("tools[2].Type = %v, want web_search", req.Params.Tools[2].Type)
	}
}

// TestSanitizeToolsForProvider_NonOpenAI_FiltersNamespace locks non-OpenAI filter behavior.
func TestSanitizeToolsForProvider_NonOpenAI_FiltersNamespace(t *testing.T) {
	log := logger.NewDefault(logger.LevelError)
	tools := sampleToolsWithNamespace()
	req := &bifrostSchemas.BifrostResponsesRequest{
		Params: &bifrostSchemas.ResponsesParameters{Tools: tools},
	}

	SanitizeToolsForProvider(req, bifrostSchemas.Anthropic, log)

	if len(req.Params.Tools) != 2 {
		t.Fatalf("Anthropic sanitize tool count: got %d want 2 (function+web_search)", len(req.Params.Tools))
	}
	for _, tool := range req.Params.Tools {
		if tool.Type == bifrostSchemas.ResponsesToolTypeNamespace {
			t.Fatal("namespace tool should be filtered for non-OpenAI providers")
		}
	}
}
