package llmgateway

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func init() {
	RegisterProvider(&ollamaProvider{})
}

type ollamaProvider struct{}

func (p *ollamaProvider) Name() string { return "ollama" }

func (p *ollamaProvider) BaseURL() string { return "http://localhost:11434" }

func (p *ollamaProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	// Ollama does not require authentication by default.
	// If apiKey is provided, set it as Bearer token (compatible with some setups).
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func (p *ollamaProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Ollama
}
