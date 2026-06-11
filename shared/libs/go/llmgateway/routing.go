package llmgateway

import (
	"sync"

	"github.com/axsh/arctic-tern/config"
	"github.com/axsh/arctic-tern/logger"
)

// RoutedModel holds the resolved provider, key value, and model name.
type RoutedModel struct {
	Provider         string // e.g. "anthropic"
	KeyName          string // e.g. "primary"
	KeyValue         string // actual API key value from profile
	Model            string // e.g. "claude-sonnet-4-20250514"
	Mode             string // "chat", "responses", or "" (treated as "chat")
	ToolCallFallback bool   // enable text-to-tool-call conversion
}

// ModelRouter resolves model names to provider/key/model using model profiles.
type ModelRouter struct {
	profiles      *config.ModelProfilesConfig
	logger        logger.Logger
	mu            sync.RWMutex
	sessionModels map[string]*RoutedModel
}

// NewModelRouter creates a ModelRouter from model profiles config.
// profiles and log may be nil.
func NewModelRouter(profiles *config.ModelProfilesConfig, log logger.Logger) *ModelRouter {
	return &ModelRouter{
		profiles:      profiles,
		logger:        log,
		sessionModels: make(map[string]*RoutedModel),
	}
}

// ResolveModel resolves a model name to a RoutedModel.
// If a sessionID is provided and the modelName is not found, it falls back to the
// first resolved model in that session.
// Returns ErrModelNotFound if the model is not defined in profiles and no fallback is available.
func (r *ModelRouter) ResolveModel(modelName string, sessionID string) (*RoutedModel, error) {
	if r.profiles == nil || modelName == "" {
		return nil, ErrModelNotFound
	}

	// 1. Try to resolve modelName from profiles.
	var resolved *RoutedModel
	for providerName, provider := range r.profiles.Providers {
		for _, key := range provider.Keys {
			for _, model := range key.Models {
				if model.Name == modelName {
					var fallback bool
					if model.Behavior != nil {
						fallback = model.Behavior.ToolCallFallback
					}
					resolved = &RoutedModel{
						Provider:         providerName,
						KeyName:          key.Name,
						KeyValue:         key.Value,
						Model:            modelName,
						Mode:             model.Mode,
						ToolCallFallback: fallback,
					}
					break
				}
			}
			if resolved != nil {
				break
			}
		}
		if resolved != nil {
			break
		}
	}

	// 2. If resolved successfully:
	if resolved != nil {
		if r.logger != nil {
			r.logger.Debug("model routing decision",
				"requested_model", modelName,
				"resolved_model", resolved.Model,
				"provider", resolved.Provider,
				"mode", resolved.Mode,
			)
			baseURL := ""
			if provider, ok := r.profiles.Providers[resolved.Provider]; ok && provider.NetworkConfig != nil {
				baseURL = provider.NetworkConfig.BaseURL
			}
			keyPrefix := MaskSecret(resolved.KeyValue)
			r.logger.Trace("routing config details",
				"key_prefix", keyPrefix,
				"base_url", baseURL,
				"fallback", resolved.ToolCallFallback,
			)
		}
		if sessionID != "" {
			r.mu.Lock()
			if _, exists := r.sessionModels[sessionID]; !exists {
				r.sessionModels[sessionID] = resolved
			}
			r.mu.Unlock()
		}
		return resolved, nil
	}

	// 3. If modelName is not found:
	if sessionID != "" {
		r.mu.RLock()
		fallbackModel, exists := r.sessionModels[sessionID]
		r.mu.RUnlock()
		if exists {
			if r.logger != nil {
				r.logger.Info("model rewrite: " + modelName + " -> " + fallbackModel.Model + " (sid=" + sessionID + ")")
			}
			return fallbackModel, nil
		}
	}

	return nil, ErrModelNotFound
}
