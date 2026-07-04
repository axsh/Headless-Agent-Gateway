package vault

import (
	"fmt"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// ChainVaultBackend tries multiple VaultStore backends in order.
// It implements VaultStore.
type ChainVaultBackend struct {
	backends []namedBackend
	logger   logger.Logger
}

// namedBackend pairs a VaultStore with its name for diagnostics.
type namedBackend struct {
	name  string
	store VaultStore
}

// NewChainVaultBackend creates a ChainVaultBackend from ordered backends.
// names and stores must have the same length.
func NewChainVaultBackend(names []string, stores []VaultStore, log logger.Logger) *ChainVaultBackend {
	backends := make([]namedBackend, len(stores))
	for i := range stores {
		backends[i] = namedBackend{name: names[i], store: stores[i]}
	}
	return &ChainVaultBackend{backends: backends, logger: log}
}

// Resolve tries each backend in order until one succeeds.
// If all fail, returns an error with details of each failure.
func (c *ChainVaultBackend) Resolve(ref string) (string, error) {
	var errs []string
	for i, nb := range c.backends {
		val, err := nb.store.Resolve(ref)
		if err == nil {
			if c.logger != nil {
				c.logger.Debug("vault ref resolved",
					"ref", ref,
					"via", nb.name,
					"tried", fmt.Sprintf("%d/%d backends", i+1, len(c.backends)))
			}
			return val, nil
		}
		errs = append(errs, fmt.Sprintf("  %d. %s: %s", i+1, nb.name, err.Error()))
	}

	return "", fmt.Errorf(
		"failed to resolve vault reference %q.\n\n"+
			"Tried %d backends in order:\n%s",
		ref, len(c.backends), strings.Join(errs, "\n"))
}

// Set stores a secret using the first backend.
func (c *ChainVaultBackend) Set(path string, value string) error {
	if len(c.backends) == 0 {
		return fmt.Errorf("no backends configured")
	}
	return c.backends[0].store.Set(path, value)
}

// Delete removes a secret from the first backend.
func (c *ChainVaultBackend) Delete(path string) error {
	if len(c.backends) == 0 {
		return fmt.Errorf("no backends configured")
	}
	return c.backends[0].store.Delete(path)
}

// List returns all secret paths from the first backend.
func (c *ChainVaultBackend) List() ([]string, error) {
	if len(c.backends) == 0 {
		return nil, fmt.Errorf("no backends configured")
	}
	return c.backends[0].store.List()
}

// Compile-time check that ChainVaultBackend implements VaultStore.
var _ VaultStore = (*ChainVaultBackend)(nil)
