package llmgateway

import (
	"net/http"
	"sync"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

// Provider abstracts provider-specific behavior (base URL, auth headers, etc.).
type Provider interface {
	// Name returns the provider identifier (e.g. "anthropic", "openai", "google", "ollama").
	Name() string

	// BaseURL returns the API base URL for this provider.
	BaseURL() string

	// SetAuthHeaders sets provider-specific authentication headers on the request.
	SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header)

	// BifrostProvider returns the corresponding Bifrost SDK ModelProvider constant.
	BifrostProvider() bifrostSchemas.ModelProvider
}

var (
	providerMu       sync.RWMutex
	providerRegistry = map[string]Provider{}
)

// RegisterProvider registers a Provider implementation.
// Typically called from init() in each provider file.
// Panics if a provider with the same name is already registered.
func RegisterProvider(p Provider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	name := p.Name()
	if _, dup := providerRegistry[name]; dup {
		panic("llmgateway: RegisterProvider called twice for " + name)
	}
	providerRegistry[name] = p
}

// GetProvider returns the Provider for the given name.
func GetProvider(name string) (Provider, bool) {
	providerMu.RLock()
	defer providerMu.RUnlock()
	p, ok := providerRegistry[name]
	return p, ok
}

// AllProviders returns all registered providers.
func AllProviders() []Provider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	providers := make([]Provider, 0, len(providerRegistry))
	for _, p := range providerRegistry {
		providers = append(providers, p)
	}
	return providers
}

// resetProviderRegistry clears the registry (for testing only).
func resetProviderRegistry() {
	providerMu.Lock()
	defer providerMu.Unlock()
	providerRegistry = map[string]Provider{}
}
