package vault

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// base62Chars defines the character set for base62 encoding.
const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// defaultTenantID is the tenant ID used when none is specified.
const defaultTenantID = "default"

// vaultMetaKey is the keyring "username" used to store the index of known keys.
const vaultMetaKey = "_vault_keys"

// toBase62 encodes a byte slice as a base62 string using math/big.
func toBase62(data []byte) string {
	n := new(big.Int).SetBytes(data)
	base := big.NewInt(62)
	mod := new(big.Int)
	var result []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		result = append(result, base62Chars[mod.Int64()])
	}
	// Reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	if len(result) == 0 {
		return "0"
	}
	return string(result)
}

// buildKeyringServiceName creates the OS Keyring service name using tenant ID hash.
func buildKeyringServiceName(tenantID ...string) string {
	tID := defaultTenantID
	if len(tenantID) > 0 && tenantID[0] != "" {
		tID = tenantID[0]
	}
	h := sha256.Sum256([]byte(strings.ToLower(tID)))
	return "tern-vault-" + toBase62(h[:])
}

// storedNode is the JSON representation persisted in the OS Keyring.
type storedNode struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KeyringVaultBackend implements VaultStore using OS Keyring
// (Windows Credential Manager, macOS Keychain, Linux Secret Service).
// It is safe for concurrent use.
type KeyringVaultBackend struct {
	serviceName string
	mu          sync.Mutex // serializes writes to prevent meta-key race conditions
}

// NewKeyringVaultBackend creates a backend scoped to the given tenant.
// If no tenantID is provided, "default" is used.
func NewKeyringVaultBackend(tenantID ...string) *KeyringVaultBackend {
	return &KeyringVaultBackend{
		serviceName: buildKeyringServiceName(tenantID...),
	}
}

// ServiceName returns the computed OS keyring service name (for testing).
func (k *KeyringVaultBackend) ServiceName() string {
	return k.serviceName
}

// Resolve resolves a vault:// reference to the actual secret from OS Keyring.
func (k *KeyringVaultBackend) Resolve(ref string) (string, error) {
	path, ok := ParseVaultRef(ref)
	if !ok {
		return "", fmt.Errorf("not a vault reference: %s", ref)
	}
	secret, err := keyring.Get(k.serviceName, path)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("key not found in keyring: %s", path)
		}
		return "", fmt.Errorf("keyring get %q: %w", path, err)
	}
	var node storedNode
	if err := json.Unmarshal([]byte(secret), &node); err != nil {
		return "", fmt.Errorf("keyring unmarshal %q: %w", path, err)
	}
	return node.Value, nil
}

// Set stores a secret at the given path in the OS Keyring.
func (k *KeyringVaultBackend) Set(path string, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	data, err := json.Marshal(storedNode{Key: path, Value: value})
	if err != nil {
		return fmt.Errorf("keyring marshal %q: %w", path, err)
	}
	if err := keyring.Set(k.serviceName, path, string(data)); err != nil {
		return fmt.Errorf("keyring set %q: %w", path, err)
	}
	return k.addToKeyIndex(path)
}

// Delete removes a secret from the OS Keyring.
func (k *KeyringVaultBackend) Delete(path string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	_ = keyring.Delete(k.serviceName, path) // best-effort delete
	keys, _ := k.loadKeyIndex()
	remaining := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != path {
			remaining = append(remaining, key)
		}
	}
	return k.saveKeyIndex(remaining)
}

// List returns all stored secret paths from the key index.
func (k *KeyringVaultBackend) List() ([]string, error) {
	keys, err := k.loadKeyIndex()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("keyring list: %w", err)
	}
	return keys, nil
}

// --- Key index helpers (caller must hold mu for write operations) ---

func (k *KeyringVaultBackend) loadKeyIndex() ([]string, error) {
	secret, err := keyring.Get(k.serviceName, vaultMetaKey)
	if err != nil {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal([]byte(secret), &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (k *KeyringVaultBackend) saveKeyIndex(keys []string) error {
	data, _ := json.Marshal(keys)
	return keyring.Set(k.serviceName, vaultMetaKey, string(data))
}

func (k *KeyringVaultBackend) addToKeyIndex(key string) error {
	keys, _ := k.loadKeyIndex()
	for _, existing := range keys {
		if existing == key {
			return nil // already indexed
		}
	}
	keys = append(keys, key)
	return k.saveKeyIndex(keys)
}

// Compile-time check that KeyringVaultBackend implements VaultStore.
var _ VaultStore = (*KeyringVaultBackend)(nil)
