package llmgateway

import (
	"context"
	"fmt"

	"github.com/axsh/arctic-tern/config"
	"github.com/axsh/arctic-tern/logger"
	"github.com/axsh/arctic-tern/vault"
	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

// fallbackProviderNameMap covers providers not yet migrated to the Provider Registry.
// These entries are used as a fallback when GetProvider() returns false.
var fallbackProviderNameMap = map[string]bifrostSchemas.ModelProvider{
	"bedrock": bifrostSchemas.Bedrock,
	"azure":   bifrostSchemas.Azure,
	"cohere":  bifrostSchemas.Cohere,
	"gemini":  bifrostSchemas.Gemini,
}

// resolveProviderName maps a tern provider name to a Bifrost ModelProvider.
// Checks the Provider Registry first, then falls back to static map.
func resolveProviderName(name string) (bifrostSchemas.ModelProvider, bool) {
	if p, ok := GetProvider(name); ok {
		return p.BifrostProvider(), true
	}
	if mp, ok := fallbackProviderNameMap[name]; ok {
		return mp, true
	}
	return "", false
}

// BifrostAccount implements bifrost schemas.Account interface
// by adapting model_profiles.yaml configuration.
type BifrostAccount struct {
	profiles *config.ModelProfilesConfig
	vault    vault.VaultStore
	logger   logger.Logger
}

// NewBifrostAccount creates a BifrostAccount from model profiles.
// profiles, vs, and log may be nil.
func NewBifrostAccount(
	profiles *config.ModelProfilesConfig,
	vs vault.VaultStore,
	log logger.Logger,
) *BifrostAccount {
	return &BifrostAccount{
		profiles: profiles,
		vault:    vs,
		logger:   log,
	}
}

// GetConfiguredProviders returns the list of configured provider keys.
func (a *BifrostAccount) GetConfiguredProviders() ([]bifrostSchemas.ModelProvider, error) {
	if a.profiles == nil {
		return nil, nil
	}

	providers := make([]bifrostSchemas.ModelProvider, 0, len(a.profiles.Providers))
	for name := range a.profiles.Providers {
		mp, ok := resolveProviderName(name)
		if !ok {
			// Use the raw name as a custom provider key
			mp = bifrostSchemas.ModelProvider(name)
		}
		providers = append(providers, mp)
	}
	return providers, nil
}

// GetKeysForProvider returns the API keys configured for a specific provider.
func (a *BifrostAccount) GetKeysForProvider(ctx context.Context, providerKey bifrostSchemas.ModelProvider) ([]bifrostSchemas.Key, error) {
	if a.profiles == nil {
		return nil, fmt.Errorf("no profiles configured")
	}

	// Find the provider config by matching the Bifrost ModelProvider key
	var provCfg *config.ProviderConfig
	for name, cfg := range a.profiles.Providers {
		mp, ok := resolveProviderName(name)
		if !ok {
			mp = bifrostSchemas.ModelProvider(name)
		}
		if mp == providerKey {
			c := cfg
			provCfg = &c
			break
		}
	}

	if provCfg == nil {
		return nil, fmt.Errorf("provider %q not found in profiles", providerKey)
	}

	keys := make([]bifrostSchemas.Key, 0, len(provCfg.ApiKeys))
	for i, keyCfg := range provCfg.ApiKeys {
		// Resolve the key value (vault:// or plain text)
		keyValue := keyCfg.Secret
		if keyValue != "" && vault.IsVaultRef(keyValue) && a.vault != nil {
			resolved, err := a.vault.Resolve(keyValue)
			if err != nil {
				if a.logger != nil {
					a.logger.Warn("failed to resolve vault ref for key %q: %v", keyCfg.Name, err)
				}
				// Use the raw vault ref as fallback (will fail at provider level)
				resolved = keyValue
			}
			keyValue = resolved
		}

		// Build model whitelist
		modelNames := make(bifrostSchemas.WhiteList, 0, len(keyCfg.Models))
		for _, m := range keyCfg.Models {
			modelNames = append(modelNames, m.Name)
		}
		// If no models specified, allow all
		if len(modelNames) == 0 {
			modelNames = bifrostSchemas.WhiteList{"*"}
		}

		weight := keyCfg.Weight
		if weight == 0 {
			weight = 1.0
		}

		key := bifrostSchemas.Key{
			ID:     fmt.Sprintf("%s-%d", keyCfg.Name, i),
			Name:   keyCfg.Name,
			Value:  bifrostSchemas.EnvVar{Val: keyValue},
			Models: modelNames,
			Weight: weight,
		}

		// Ollama requires OllamaKeyConfig with the server URL.
		if providerKey == bifrostSchemas.Ollama {
			baseURL := "http://localhost:11434"
			if p, ok := GetProvider("ollama"); ok {
				baseURL = p.BaseURL()
			}
			key.OllamaKeyConfig = &bifrostSchemas.OllamaKeyConfig{
				URL: bifrostSchemas.EnvVar{Val: baseURL},
			}
		}

		keys = append(keys, key)
	}

	return keys, nil
}

// GetConfigForProvider returns the Bifrost provider config for a given provider.
func (a *BifrostAccount) GetConfigForProvider(providerKey bifrostSchemas.ModelProvider) (*bifrostSchemas.ProviderConfig, error) {
	if a.profiles == nil {
		return nil, fmt.Errorf("no profiles configured")
	}

	// Find the provider config
	var provCfg *config.ProviderConfig
	for name, cfg := range a.profiles.Providers {
		mp, ok := resolveProviderName(name)
		if !ok {
			mp = bifrostSchemas.ModelProvider(name)
		}
		if mp == providerKey {
			c := cfg
			provCfg = &c
			break
		}
	}

	if provCfg == nil {
		return nil, fmt.Errorf("provider %q not found in profiles", providerKey)
	}

	cfg := &bifrostSchemas.ProviderConfig{
		NetworkConfig:            bifrostSchemas.DefaultNetworkConfig,
		ConcurrencyAndBufferSize: bifrostSchemas.DefaultConcurrencyAndBufferSize,
	}

	// Apply network config overrides from profiles
	if provCfg.NetworkConfig != nil {
		if provCfg.NetworkConfig.BaseURL != "" {
			cfg.NetworkConfig.BaseURL = provCfg.NetworkConfig.BaseURL
		}
		if provCfg.NetworkConfig.RequestTimeoutSeconds > 0 {
			cfg.NetworkConfig.DefaultRequestTimeoutInSeconds = provCfg.NetworkConfig.RequestTimeoutSeconds
		}
	}

	return cfg, nil
}

// Compile-time check that BifrostAccount implements schemas.Account.
var _ bifrostSchemas.Account = (*BifrostAccount)(nil)
