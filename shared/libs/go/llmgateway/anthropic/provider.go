package anthropic

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
)

func init() {
	llmgateway.RegisterProvider(&Provider{})
	handlerctx.RegisterHandler("POST /v1/messages", HandleMessages)
}

// Provider implements llmgateway.Provider for Anthropic.
type Provider struct{}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) BaseURL() string { return "https://api.anthropic.com" }

func (p *Provider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
}

func (p *Provider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Anthropic
}
