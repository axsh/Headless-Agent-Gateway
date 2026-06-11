package llmgateway

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func init() {
	RegisterProvider(&openaiProvider{})
}

type openaiProvider struct{}

func (p *openaiProvider) Name() string { return "openai" }

func (p *openaiProvider) BaseURL() string { return "https://api.openai.com" }

func (p *openaiProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func (p *openaiProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.OpenAI
}
