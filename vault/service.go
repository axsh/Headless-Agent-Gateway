package vault

import "fmt"

// NewService creates a vault Service with the given store.
func NewService(store interface {
	Resolve(ref string) (string, error)
	Set(path string, value string) error
	Delete(path string) error
	List() ([]string, error)
}) *Service {
	return &Service{store: store}
}

// ResolveKey converts provider shorthand or direct key to a full vault key path.
func ResolveKey(provider, key string) (string, error) {
	if provider != "" {
		return "providers/" + provider + "/default", nil
	}
	if key != "" {
		return key, nil
	}
	return "", ErrKeyRequired
}

// Set stores a secret value and returns the resolved key.
func (s *Service) Set(provider, key, value string) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrStoreRequired
	}
	if value == "" {
		return "", ErrValueRequired
	}
	fullKey, err := ResolveKey(provider, key)
	if err != nil {
		return "", err
	}
	if err := s.store.Set(fullKey, value); err != nil {
		return "", fmt.Errorf("set failed: %w", err)
	}
	return fullKey, nil
}

// Get resolves a secret key and returns registration status or value.
func (s *Service) Get(provider, key string, reveal bool) (GetResult, error) {
	if s == nil || s.store == nil {
		return GetResult{}, ErrStoreRequired
	}
	fullKey, err := ResolveKey(provider, key)
	if err != nil {
		return GetResult{}, err
	}
	val, err := s.store.Resolve("vault://" + fullKey)
	if err != nil {
		if reveal {
			return GetResult{}, fmt.Errorf("%s is not registered", fullKey)
		}
		return GetResult{
			FullKey:    fullKey,
			Registered: false,
		}, nil
	}
	res := GetResult{
		FullKey:    fullKey,
		Registered: true,
	}
	if reveal {
		res.Value = val
	}
	return res, nil
}

// Delete removes a secret and returns the resolved key.
func (s *Service) Delete(provider, key string) (string, error) {
	if s == nil || s.store == nil {
		return "", ErrStoreRequired
	}
	fullKey, err := ResolveKey(provider, key)
	if err != nil {
		return "", err
	}
	if err := s.store.Delete(fullKey); err != nil {
		return "", fmt.Errorf("delete failed: %w", err)
	}
	return fullKey, nil
}

// List returns all secret paths stored in Vault.
func (s *Service) List() ([]string, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreRequired
	}
	return s.store.List()
}

// Status returns registration state of providers/default keys.
func (s *Service) Status(providers []string) ([]ProviderState, error) {
	if s == nil || s.store == nil {
		return nil, ErrStoreRequired
	}
	out := make([]ProviderState, 0, len(providers))
	for _, provider := range providers {
		fullKey := "providers/" + provider + "/default"
		_, err := s.store.Resolve("vault://" + fullKey)
		out = append(out, ProviderState{
			Provider:   provider,
			Registered: err == nil,
		})
	}
	return out, nil
}
