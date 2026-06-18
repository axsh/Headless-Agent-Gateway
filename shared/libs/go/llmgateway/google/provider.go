package google

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

func init() {
	llmgateway.RegisterProvider(&Provider{})
}

// Provider implements llmgateway.Provider for Google (Gemini).
type Provider struct{}

func (p *Provider) Name() string { return "google" }

func (p *Provider) BaseURL() string { return "https://generativelanguage.googleapis.com" }

func (p *Provider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Del("Authorization")
	// URL query parameter is intentionally NOT set to prevent API key exposure in logs.
}

func (p *Provider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Gemini
}
