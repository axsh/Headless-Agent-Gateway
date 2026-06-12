package ollama

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/llmgateway"
)

func init() {
	llmgateway.RegisterProvider(&Provider{})
}

// Provider implements llmgateway.Provider for Ollama.
type Provider struct{}

func (p *Provider) Name() string { return "ollama" }

func (p *Provider) BaseURL() string { return "http://localhost:11434" }

func (p *Provider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	// Ollama does not require authentication by default.
	// If apiKey is provided, set it as Bearer token (compatible with some setups).
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func (p *Provider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Ollama
}
