package vault

// VaultStore manages secret storage and retrieval.
type VaultStore interface {
	// Resolve resolves a vault:// reference to the actual secret value.
	// Returns an error if the reference cannot be resolved.
	Resolve(ref string) (string, error)

	// Set stores a secret at the given path.
	Set(path string, value string) error

	// Delete removes a secret at the given path.
	Delete(path string) error

	// List returns all stored secret paths.
	List() ([]string, error)
}
