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
	// Google also accepts API key as query parameter.
	if req.URL.RawQuery != "" {
		req.URL.RawQuery = req.URL.RawQuery + "&key=" + apiKey
	} else {
		req.URL.RawQuery = "key=" + apiKey
	}
}

func (p *googleProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Gemini
}
