package llmgateway

import (
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/logger"
)

// sanitizeToolsForProvider filters and adjusts tools in a Bifrost request
// for cross-provider compatibility. Two issues are addressed:
//
//  1. Codex CLI sends OpenAI-specific tool types (e.g. "namespace") that non-OpenAI
//     providers cannot handle.
//  2. Gemini API does not support function_declarations and google_search together.
//     Bifrost SDK's convertResponsesToolsToGemini skips function tools when web_search
//     is present, but still sets FunctionCallingConfig from ToolChoice, causing
//     "Function calling config without function_declarations".
//
// Fix: Filter to compatible tool types, then for Gemini handle the mutual exclusion
// between web_search and function tools (prioritizing function tools).
func sanitizeToolsForProvider(
	bifrostReq *bifrostSchemas.BifrostResponsesRequest,
	providerKey bifrostSchemas.ModelProvider,
	log logger.Logger,
) {
	if bifrostReq.Params == nil || providerKey == bifrostSchemas.OpenAI {
		return
	}

	var compatibleTools []bifrostSchemas.ResponsesTool
	hasWebSearch := false
	hasFunctionTools := false
	for _, tool := range bifrostReq.Params.Tools {
		switch tool.Type {
		case bifrostSchemas.ResponsesToolTypeFunction:
			compatibleTools = append(compatibleTools, tool)
			hasFunctionTools = true
		case bifrostSchemas.ResponsesToolTypeWebSearch,
			bifrostSchemas.ResponsesToolTypeWebSearchPreview:
			compatibleTools = append(compatibleTools, tool)
			hasWebSearch = true
		default:
			log.Debug("filtering unsupported tool type for provider",
				"tool_type", tool.Type, "provider", providerKey)
		}
	}

	// Gemini does not support function_declarations + google_search together.
	// When both are present, keep function tools (needed for Codex CLI operations)
	// and drop web_search. Function calling is the primary capability.
	if hasWebSearch && hasFunctionTools && providerKey == bifrostSchemas.Gemini {
		var functionOnly []bifrostSchemas.ResponsesTool
		for _, tool := range compatibleTools {
			if tool.Type == bifrostSchemas.ResponsesToolTypeFunction {
				functionOnly = append(functionOnly, tool)
			}
		}
		compatibleTools = functionOnly
		log.Debug("gemini: dropped web_search tools due to function tool coexistence",
			"kept_tools", len(functionOnly))
	}

	if len(compatibleTools) == 0 {
		bifrostReq.Params.Tools = nil
		bifrostReq.Params.ToolChoice = nil
		log.Debug("cleared all tools and tool_choice: no provider-compatible tools",
			"provider", providerKey)
	} else {
		bifrostReq.Params.Tools = compatibleTools
	}
}
