package llmgateway

import (
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

// ToBifrostProvider converts tern provider name to Bifrost ModelProvider.
// Uses the Provider Registry first, then falls back to static mapping.
func ToBifrostProvider(provider string) bifrostSchemas.ModelProvider {
	if mp, ok := resolveProviderName(provider); ok {
		return mp
	}
	return bifrostSchemas.ModelProvider(provider)
}
