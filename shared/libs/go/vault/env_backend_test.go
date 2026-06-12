package vault

import (
	"sort"
	"strings"
	"testing"
)

// Compile-time check: EnvVaultBackend implements VaultStore.
var _ VaultStore = (*EnvVaultBackend)(nil)

func TestEnvVaultBackend_PathToEnvName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"providers/anthropic/primary", "HAG_VAULT_ANTHROPIC_PRIMARY"},
		{"providers/openai/team-a", "HAG_VAULT_OPENAI_TEAM_A"},
		{"providers/ollama/default", "HAG_VAULT_OLLAMA_DEFAULT"},
		{"custom/path/key", "HAG_VAULT_CUSTOM_PATH_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := pathToEnvName(tt.path); got != tt.want {
				t.Errorf("pathToEnvName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEnvVaultBackend_Resolve(t *testing.T) {
	backend := NewEnvVaultBackend()

	t.Run("success", func(t *testing.T) {
		t.Setenv("HAG_VAULT_ANTHROPIC_PRIMARY", "sk-ant-test123")
		val, err := backend.Resolve("vault://providers/anthropic/primary")
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if val != "sk-ant-test123" {
			t.Errorf("Resolve() = %q, want %q", val, "sk-ant-test123")
		}
	})

	t.Run("missing env var", func(t *testing.T) {
		_, err := backend.Resolve("vault://providers/openai/missing-key")
		if err == nil {
			t.Fatal("expected error for missing env var")
		}
		if !strings.Contains(err.Error(), "HAG_VAULT_OPENAI_MISSING_KEY") {
			t.Errorf("error should mention env var name: %v", err)
		}
	})

	t.Run("not a vault ref", func(t *testing.T) {
		_, err := backend.Resolve("plaintext-key")
		if err == nil {
			t.Fatal("expected error for non-vault ref")
		}
	})
}

func TestEnvVaultBackend_Set(t *testing.T) {
	backend := NewEnvVaultBackend()
	if err := backend.Set("providers/anthropic/test", "secret-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	val, err := backend.Resolve("vault://providers/anthropic/test")
	if err != nil {
		t.Fatalf("Resolve() after Set() error = %v", err)
	}
	if val != "secret-value" {
		t.Errorf("Resolve() = %q, want %q", val, "secret-value")
	}
}

func TestEnvVaultBackend_Delete(t *testing.T) {
	backend := NewEnvVaultBackend()
	t.Setenv("HAG_VAULT_ANTHROPIC_DELETE", "to-delete")

	if err := backend.Delete("providers/anthropic/delete"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err := backend.Resolve("vault://providers/anthropic/delete")
	if err == nil {
		t.Fatal("expected error after Delete")
	}
}

func TestEnvVaultBackend_List(t *testing.T) {
	backend := NewEnvVaultBackend()
	t.Setenv("HAG_VAULT_ANTHROPIC_LIST1", "key1")
	t.Setenv("HAG_VAULT_OPENAI_LIST2", "key2")

	paths, err := backend.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Filter to just our test entries (test env may have other HAG_VAULT_ vars).
	var found []string
	for _, p := range paths {
		if strings.Contains(p, "list") {
			found = append(found, p)
		}
	}
	sort.Strings(found)

	if len(found) < 2 {
		t.Fatalf("expected at least 2 paths, got %d: %v", len(found), found)
	}
}

func TestEnvVaultBackend_MultiTenant(t *testing.T) {
	backend := NewEnvVaultBackend()
	t.Setenv("HAG_VAULT_ANTHROPIC_TEAM_A", "key-team-a")
	t.Setenv("HAG_VAULT_ANTHROPIC_TEAM_B", "key-team-b")

	valA, err := backend.Resolve("vault://providers/anthropic/team-a")
	if err != nil {
		t.Fatalf("Resolve team-a error = %v", err)
	}
	valB, err := backend.Resolve("vault://providers/anthropic/team-b")
	if err != nil {
		t.Fatalf("Resolve team-b error = %v", err)
	}

	if valA == valB {
		t.Errorf("team-a and team-b should have different values: %q", valA)
	}
	if valA != "key-team-a" {
		t.Errorf("team-a = %q, want %q", valA, "key-team-a")
	}
	if valB != "key-team-b" {
		t.Errorf("team-b = %q, want %q", valB, "key-team-b")
	}
}
