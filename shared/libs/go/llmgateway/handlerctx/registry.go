package handlerctx

import (
	"net/http"
	"sync"
)

// HandlerFactory creates an http.HandlerFunc from a HandlerContext.
type HandlerFactory func(ctx HandlerContext) http.HandlerFunc

var (
	handlerMu       sync.RWMutex
	handlerRegistry = map[string]HandlerFactory{}
)

// RegisterHandler registers a handler factory for a route path.
// Typically called from init() in each handler subpackage.
func RegisterHandler(path string, factory HandlerFactory) {
	handlerMu.Lock()
	defer handlerMu.Unlock()
	handlerRegistry[path] = factory
}

// AllHandlers returns a copy of all registered handler factories.
func AllHandlers() map[string]HandlerFactory {
	handlerMu.RLock()
	defer handlerMu.RUnlock()
	result := make(map[string]HandlerFactory, len(handlerRegistry))
	for k, v := range handlerRegistry {
		result[k] = v
	}
	return result
}
