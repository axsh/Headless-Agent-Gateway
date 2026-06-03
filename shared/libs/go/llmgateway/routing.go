package llmgateway

import (
	"github.com/axsh/hag/config"
	"github.com/axsh/hag/logger"
)

// RoutedModel holds the resolved provider, key value, and model name.
type RoutedModel struct {
	Provider string // e.g. "anthropic"
	KeyName  string // e.g. "primary"
	KeyValue string // actual API key value from profile
	Model    string // e.g. "claude-sonnet-4-20250514"
}

// ModelRouter resolves model names to provider/key/model using model profiles.
type ModelRouter struct {
	profiles *config.ModelProfilesConfig
	logger   logger.Logger
}

// NewModelRouter creates a ModelRouter from model profiles config.
// profiles and log may be nil.
func NewModelRouter(profiles *config.ModelProfilesConfig, log logger.Logger) *ModelRouter {
	return &ModelRouter{
		profiles: profiles,
		logger:   log,
	}
}

// ResolveModel resolves a model name to a RoutedModel.
// Returns ErrModelNotFound if the model is not defined in profiles.
func (r *ModelRouter) ResolveModel(modelName string) (*RoutedModel, error) {
	if r.profiles == nil || modelName == "" {
		return nil, ErrModelNotFound
	}

	for providerName, provider := range r.profiles.Providers {
		for _, key := range provider.Keys {
			for _, model := range key.Models {
				if model.Name == modelName {
					return &RoutedModel{
						Provider: providerName,
						KeyName:  key.Name,
						KeyValue: key.Value,
						Model:    modelName,
					}, nil
				}
			}
		}
	}

	return nil, ErrModelNotFound
}
