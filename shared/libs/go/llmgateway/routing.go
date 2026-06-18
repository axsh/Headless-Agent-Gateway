package llmgateway

import (
	"sync"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway/handlerctx"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// RoutedModel is an alias for handlerctx.RoutedModel for backward compatibility.
type RoutedModel = handlerctx.RoutedModel

type sessionEntry struct {
	model    *RoutedModel
	lastUsed time.Time
}

// ModelRouter resolves model names to provider/key/model using model profiles.
type ModelRouter struct {
	profiles      *config.ModelProfilesConfig
	logger        logger.Logger
	mu            sync.RWMutex
	sessionModels map[string]*sessionEntry
	maxSessions   int
	sessionTTL    time.Duration
	accessOrder   []string // LRU order: oldest first
}

// NewModelRouter creates a ModelRouter from model profiles config.
// profiles and log may be nil.
func NewModelRouter(profiles *config.ModelProfilesConfig, cfg *config.AppConfig, log logger.Logger) *ModelRouter {
	maxSessions := 0
	var sessionTTL time.Duration
	if cfg != nil {
		maxSessions = cfg.LLMGateway.Session.MaxSessions
		sessionTTL = time.Duration(cfg.LLMGateway.Session.TTLSeconds) * time.Second
	}
	return &ModelRouter{
		profiles:      profiles,
		logger:        log,
		sessionModels: make(map[string]*sessionEntry),
		maxSessions:   maxSessions,
		sessionTTL:    sessionTTL,
		accessOrder:   make([]string, 0),
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
				// Evict oldest if at capacity
				if r.maxSessions > 0 && len(r.sessionModels) >= r.maxSessions {
					r.evictOldest()
				}
				r.sessionModels[sessionID] = &sessionEntry{
					model:    resolved,
					lastUsed: time.Now(),
				}
				r.accessOrder = append(r.accessOrder, sessionID)
			} else {
				// Update LRU position
				r.sessionModels[sessionID].lastUsed = time.Now()
				r.touchAccessOrder(sessionID)
			}
			r.mu.Unlock()
		}
		return resolved, nil
	}

	// 3. If modelName is not found:
	if sessionID != "" {
		r.mu.RLock()
		entry, exists := r.sessionModels[sessionID]
		r.mu.RUnlock()
		if exists {
			// TTL check
			if r.sessionTTL > 0 && time.Since(entry.lastUsed) > r.sessionTTL {
				r.mu.Lock()
				delete(r.sessionModels, sessionID)
				r.removeFromAccessOrder(sessionID)
				r.mu.Unlock()
				if r.logger != nil {
					r.logger.Debug("session expired (TTL)", "sid", sessionID)
				}
				// Fall through to "not found" error
			} else {
				r.mu.Lock()
				entry.lastUsed = time.Now()
				r.touchAccessOrder(sessionID)
				r.mu.Unlock()
				if r.logger != nil {
					r.logger.Info("model rewrite: " + modelName + " -> " + entry.model.Model + " (sid=" + sessionID + ")")
				}
				return entry.model, nil
			}
		}
	}

	return nil, ErrModelNotFound
}

func (r *ModelRouter) evictOldest() {
	if len(r.accessOrder) == 0 {
		return
	}
	oldest := r.accessOrder[0]
	r.accessOrder = r.accessOrder[1:]
	delete(r.sessionModels, oldest)
	if r.logger != nil {
		r.logger.Debug("session evicted (max capacity)", "sid", oldest)
	}
}

func (r *ModelRouter) touchAccessOrder(sessionID string) {
	r.removeFromAccessOrder(sessionID)
	r.accessOrder = append(r.accessOrder, sessionID)
}

func (r *ModelRouter) removeFromAccessOrder(sessionID string) {
	for i, sid := range r.accessOrder {
		if sid == sessionID {
			r.accessOrder = append(r.accessOrder[:i], r.accessOrder[i+1:]...)
			break
		}
	}
}
