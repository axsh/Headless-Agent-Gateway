package llmgateway

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func init() {
	RegisterProvider(&anthropicProvider{})
}

type anthropicProvider struct{}

func (p *anthropicProvider) Name() string { return "anthropic" }

func (p *anthropicProvider) BaseURL() string { return "https://api.anthropic.com" }

func (p *anthropicProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
}

func (p *anthropicProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Anthropic
}
