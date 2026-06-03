package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileVaultBackend_Lifecycle(t *testing.T) {
	// Set AES key for test
	os.Setenv("HAG_VAULT_KEY", "12345678901234567890123456789012") // 32 bytes key
	defer os.Unsetenv("HAG_VAULT_KEY")

	tmpDir, err := os.MkdirTemp("", "hag-vault-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "vault.db")

	backend, err := NewFileVaultBackend(dbPath)
	if err != nil {
		t.Fatalf("NewFileVaultBackend failed: %v", err)
	}

	secretPath := "providers/anthropic/default"
	secretVal := "sk-ant-test-key"

	// Set secret
	err = backend.Set(secretPath, secretVal)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify file is encrypted (should not contain plaintext key)
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	content := string(data)
	if strings.Contains(content, secretVal) {
		t.Errorf("expected file content to be encrypted, but it contains plaintext %q", secretVal)
	}

	// Resolve secret
	resolved, err := backend.Resolve("vault://" + secretPath)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if resolved != secretVal {
		t.Errorf("expected resolved %q, got %q", secretVal, resolved)
	}

	// List
	list, err := backend.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(list) != 1 || list[0] != secretPath {
		t.Errorf("expected list to contain %q, got %v", secretPath, list)
	}

	// Delete
	err = backend.Delete(secretPath)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, err = backend.Resolve("vault://" + secretPath)
	if err == nil {
		t.Errorf("expected error resolving deleted secret")
	}
}
