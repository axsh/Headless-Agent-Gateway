package llmgateway

import (
	bifrost "github.com/maximhq/bifrost/core"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/shared/libs/go/vault"
)

// Compile-time check: ProxyServer implements handlerctx.HandlerContext.
var _ handlerctx.HandlerContext = (*ProxyServer)(nil)

// Config returns the application config.
func (p *ProxyServer) Config() *config.AppConfig { return p.cfg }

// Logger returns the logger instance.
func (p *ProxyServer) Logger() logger.Logger { return p.logger }

// Vault returns the vault store (may be nil).
func (p *ProxyServer) Vault() vault.VaultStore { return p.vault }

// BifrostSDK returns the Bifrost SDK instance (may be nil).
func (p *ProxyServer) BifrostSDK() *bifrost.Bifrost {
	if p.driver == nil {
		return nil
	}
	return p.driver.bifrostSDK
}

// Router returns the model router (may be nil).
// Returns handlerctx.ModelRouter to satisfy the HandlerContext interface.
func (p *ProxyServer) Router() handlerctx.ModelRouter {
	if p.driver == nil || p.driver.router == nil {
		return nil
	}
	return p.driver.router
}

// ToBifrostProvider converts a tern provider name to Bifrost ModelProvider.
func (p *ProxyServer) ToBifrostProvider(provider string) bifrostSchemas.ModelProvider {
	return ToBifrostProvider(provider)
}

// SanitizeTools filters tools in a Bifrost request for cross-provider compatibility.
func (p *ProxyServer) SanitizeTools(req *bifrostSchemas.BifrostResponsesRequest, provider bifrostSchemas.ModelProvider) {
	SanitizeToolsForProvider(req, provider, p.logger)
}

// TryFallbackAnthropicResponse applies tool call fallback rewriting.
func (p *ProxyServer) TryFallbackAnthropicResponse(body []byte) ([]byte, bool) {
	return TryFallbackAnthropicResponse(body)
}

// ExtractSessionID extracts the session ID from an auth header value.
func (p *ProxyServer) ExtractSessionID(authHeader string) string {
	return ExtractSessionID(authHeader)
}

// ExtractFallbackFlag extracts the fallback flag from an auth header value.
func (p *ProxyServer) ExtractFallbackFlag(authHeader string) bool {
	return ExtractFallbackFlag(authHeader)
}

// MaskSecret masks a secret string for logging.
func (p *ProxyServer) MaskSecret(s string) string {
	return MaskSecret(s)
}
