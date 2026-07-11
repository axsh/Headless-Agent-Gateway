package vault

import (
	"errors"
	"io"

	sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
)

var (
	// ErrStoreRequired is returned when Service or CLIRunner is created without a store.
	ErrStoreRequired = errors.New("vault store is required")
	// ErrKeyRequired is returned when neither provider nor key is specified.
	ErrKeyRequired = errors.New("either --provider or --key is required")
	// ErrValueRequired is returned when set command receives an empty secret value.
	ErrValueRequired = errors.New("value is required")
)

// Service wraps a VaultStore and exposes simple operations for external tools.
type Service struct {
	store sharedvault.VaultStore
}

// GetResult is the result of a get operation.
type GetResult struct {
	FullKey    string
	Registered bool
	Value      string
}

// ProviderState represents registration state for one provider key.
type ProviderState struct {
	Provider   string
	Registered bool
}

// CLIConfig configures CLIRunner behavior and IO.
type CLIConfig struct {
	Store          sharedvault.VaultStore
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	KnownProviders []string
	AppName        string
	AppVersion     string
}
