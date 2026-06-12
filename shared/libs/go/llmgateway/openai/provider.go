package openai

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/llmgateway"
	"github.com/axsh/arctic-tern/llmgateway/handlerctx"
)

func init() {
	llmgateway.RegisterProvider(&Provider{})
	handlerctx.RegisterHandler("POST /v1/responses", HandleResponses)
}

// Provider implements llmgateway.Provider for OpenAI.
type Provider struct{}

func (p *Provider) Name() string { return "openai" }

func (p *Provider) BaseURL() string { return "https://api.openai.com" }

func (p *Provider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func (p *Provider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.OpenAI
}
