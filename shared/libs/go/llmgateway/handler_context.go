package llmgateway

import (
	bifrost "github.com/maximhq/bifrost/core"

	"github.com/axsh/arctic-tern/config"
	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/vault"
)

// HandlerContext provides handler-level access to ProxyServer internals.
// Subpackage handlers receive this instead of a *ProxyServer reference,
// enabling handlers to live in separate packages without circular imports.
type HandlerContext interface {
	// Config returns the application config.
	Config() *config.AppConfig
	// Logger returns the logger instance.
	Logger() logger.Logger
	// Vault returns the vault store (may be nil).
	Vault() vault.VaultStore
	// Router returns the model router (may be nil).
	Router() *ModelRouter
	// BifrostSDK returns the Bifrost SDK instance (may be nil).
	BifrostSDK() *bifrost.Bifrost
}

// Compile-time check: ProxyServer implements HandlerContext.
var _ HandlerContext = (*ProxyServer)(nil)

// Config returns the application config.
func (p *ProxyServer) Config() *config.AppConfig { return p.cfg }

// Logger returns the logger instance.
func (p *ProxyServer) Logger() logger.Logger { return p.logger }

// Vault returns the vault store (may be nil).
func (p *ProxyServer) Vault() vault.VaultStore { return p.vault }

// Router returns the model router (may be nil).
func (p *ProxyServer) Router() *ModelRouter {
	if p.driver == nil {
		return nil
	}
	return p.driver.router
}

// BifrostSDK returns the Bifrost SDK instance (may be nil).
func (p *ProxyServer) BifrostSDK() *bifrost.Bifrost {
	if p.driver == nil {
		return nil
	}
	return p.driver.bifrostSDK
}
