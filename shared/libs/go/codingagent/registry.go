package codingagent

import "sync"

// FactoryFunc creates a CodingAgent from config.
// Return (nil, nil) if the agent's CLI is not available (graceful skip).
// Return (nil, err) if initialization fails unexpectedly.
type FactoryFunc func(cfg *AdapterConfig) (CodingAgent, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]FactoryFunc{}
)

// Register registers a factory function for the given agent name.
// Typically called from init() in each adapter's package.
// Panics if name is already registered (programming error).
func Register(name string, factory FactoryFunc) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		panic("codingagent: Register called twice for " + name)
	}
	registry[name] = factory
}

// CreateAll creates all registered agents using the given config.
// Agents whose factory returns (nil, nil) are silently skipped (CLI not found).
// Agents whose factory returns an error are logged and skipped.
// Returns the successfully created agents.
func CreateAll(cfg *AdapterConfig) []CodingAgent {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var agents []CodingAgent
	for name, factory := range registry {
		agent, err := factory(cfg)
		if err != nil {
			if cfg.Logger != nil {
				cfg.Logger.Warn("failed to create coding agent",
					"agent", name, "error", err.Error())
			}
			continue
		}
		if agent == nil {
			// CLI not found, skip silently
			continue
		}
		agents = append(agents, agent)
	}
	return agents
}

// resetRegistry clears the registry (for testing only).
func resetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]FactoryFunc{}
}
