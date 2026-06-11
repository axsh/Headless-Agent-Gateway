package llmgateway

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func init() {
	RegisterProvider(&googleProvider{})
}

type googleProvider struct{}

func (p *googleProvider) Name() string { return "google" }

func (p *googleProvider) BaseURL() string { return "https://generativelanguage.googleapis.com" }

func (p *googleProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Del("Authorization")
	// URL query parameter is intentionally NOT set to prevent API key exposure in logs.
}

func (p *googleProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Gemini
}
