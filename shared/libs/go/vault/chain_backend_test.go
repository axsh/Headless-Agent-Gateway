package vault

import (
	"fmt"
	"strings"
	"testing"
)

// stubVault is a test double for VaultStore.
type stubVault struct {
	secrets map[string]string
}

func newStubVault(secrets map[string]string) *stubVault {
	return &stubVault{secrets: secrets}
}

func (s *stubVault) Resolve(ref string) (string, error) {
	path, ok := ParseVaultRef(ref)
	if !ok {
		return "", fmt.Errorf("not a vault reference: %s", ref)
	}
	val, exists := s.secrets[path]
	if !exists {
		return "", fmt.Errorf("key not found: %s", path)
	}
	return val, nil
}

func (s *stubVault) Set(path, value string) error {
	s.secrets[path] = value
	return nil
}

func (s *stubVault) Delete(path string) error {
	delete(s.secrets, path)
	return nil
}

func (s *stubVault) List() ([]string, error) {
	keys := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}

// Compile-time check: ChainVaultBackend implements VaultStore.
var _ VaultStore = (*ChainVaultBackend)(nil)

func TestChainVaultBackend_FirstSuccess(t *testing.T) {
	store1 := newStubVault(map[string]string{"providers/openai/default": "sk-first"})
	store2 := newStubVault(map[string]string{"providers/openai/default": "sk-second"})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	val, err := chain.Resolve("vault://providers/openai/default")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if val != "sk-first" {
		t.Errorf("Resolve() = %q, want %q (first backend should win)", val, "sk-first")
	}
}

func TestChainVaultBackend_Fallback(t *testing.T) {
	store1 := newStubVault(map[string]string{}) // empty - will fail
	store2 := newStubVault(map[string]string{"providers/openai/default": "sk-fallback"})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	val, err := chain.Resolve("vault://providers/openai/default")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if val != "sk-fallback" {
		t.Errorf("Resolve() = %q, want %q (should fall back to second backend)", val, "sk-fallback")
	}
}

func TestChainVaultBackend_AllFail(t *testing.T) {
	store1 := newStubVault(map[string]string{})
	store2 := newStubVault(map[string]string{})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	_, err := chain.Resolve("vault://providers/openai/default")
	if err == nil {
		t.Fatal("expected error when all backends fail")
	}
	// Verify error contains both backend failures.
	if !strings.Contains(err.Error(), "keyring") {
		t.Errorf("error should mention 'keyring': %v", err)
	}
	if !strings.Contains(err.Error(), "env") {
		t.Errorf("error should mention 'env': %v", err)
	}
}

func TestChainVaultBackend_AllFail_ErrorFormat(t *testing.T) {
	store1 := newStubVault(map[string]string{})
	store2 := newStubVault(map[string]string{})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	_, err := chain.Resolve("vault://providers/openai/default")
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "Tried 2 backends in order") {
		t.Errorf("error should contain 'Tried 2 backends in order': %v", msg)
	}
	if !strings.Contains(msg, "1. keyring:") {
		t.Errorf("error should contain '1. keyring:': %v", msg)
	}
	if !strings.Contains(msg, "2. env:") {
		t.Errorf("error should contain '2. env:': %v", msg)
	}
}

func TestChainVaultBackend_Set_FirstOnly(t *testing.T) {
	store1 := newStubVault(map[string]string{})
	store2 := newStubVault(map[string]string{})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	if err := chain.Set("providers/openai/default", "sk-new"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// First backend should have the secret.
	val, err := store1.Resolve("vault://providers/openai/default")
	if err != nil {
		t.Fatalf("store1.Resolve() error = %v", err)
	}
	if val != "sk-new" {
		t.Errorf("store1 value = %q, want %q", val, "sk-new")
	}

	// Second backend should NOT have the secret.
	_, err = store2.Resolve("vault://providers/openai/default")
	if err == nil {
		t.Error("store2 should not have the secret (Set should only write to first)")
	}
}

func TestChainVaultBackend_Delete_FirstOnly(t *testing.T) {
	store1 := newStubVault(map[string]string{"providers/openai/default": "sk-delete"})
	store2 := newStubVault(map[string]string{"providers/openai/default": "sk-keep"})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	if err := chain.Delete("providers/openai/default"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// First backend should no longer have the secret.
	_, err := store1.Resolve("vault://providers/openai/default")
	if err == nil {
		t.Error("store1 should not have the secret after Delete")
	}

	// Second backend should still have it.
	val, err := store2.Resolve("vault://providers/openai/default")
	if err != nil {
		t.Fatalf("store2.Resolve() error = %v", err)
	}
	if val != "sk-keep" {
		t.Errorf("store2 value = %q, want %q", val, "sk-keep")
	}
}

func TestChainVaultBackend_List_FirstOnly(t *testing.T) {
	store1 := newStubVault(map[string]string{
		"providers/openai/default":    "sk-1",
		"providers/anthropic/primary": "sk-2",
	})
	store2 := newStubVault(map[string]string{
		"providers/google/default": "sk-3",
	})

	chain := NewChainVaultBackend(
		[]string{"keyring", "env"},
		[]VaultStore{store1, store2},
		nil,
	)

	paths, err := chain.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Should return only store1 paths (2 keys).
	if len(paths) != 2 {
		t.Errorf("List() returned %d paths, want 2", len(paths))
	}

	// Store2's key should not appear.
	for _, p := range paths {
		if p == "providers/google/default" {
			t.Error("List() should not return store2 paths")
		}
	}
}
