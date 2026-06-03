package vault

import (
	"os"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	// Use go-keyring's in-memory mock so tests don't touch the real OS keyring.
	keyring.MockInit()
	os.Exit(m.Run())
}

func TestBuildKeyringServiceName(t *testing.T) {
	t.Run("deterministic for same tenant", func(t *testing.T) {
		a := buildKeyringServiceName()
		b := buildKeyringServiceName()
		if a != b {
			t.Errorf("expected same name, got %q and %q", a, b)
		}
	})

	t.Run("different tenants produce different names", func(t *testing.T) {
		a := buildKeyringServiceName("tenant-a")
		b := buildKeyringServiceName("tenant-b")
		if a == b {
			t.Errorf("expected different names for different tenants, got %q", a)
		}
	})

	t.Run("starts with hag-vault- prefix", func(t *testing.T) {
		name := buildKeyringServiceName()
		prefix := "hag-vault-"
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			t.Errorf("expected prefix %q, got %q", prefix, name)
		}
	})
}

func TestToBase62(t *testing.T) {
	t.Run("result contains only base62 chars", func(t *testing.T) {
		result := toBase62([]byte("hello world"))
		for _, c := range result {
			found := false
			for _, valid := range base62Chars {
				if c == valid {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("unexpected char %c in base62 result %q", c, result)
			}
		}
	})

	t.Run("different inputs produce different outputs", func(t *testing.T) {
		a := toBase62([]byte("input-a"))
		b := toBase62([]byte("input-b"))
		if a == b {
			t.Errorf("expected different outputs, got %q", a)
		}
	})

	t.Run("empty input returns zero", func(t *testing.T) {
		result := toBase62([]byte{})
		if result != "0" {
			t.Errorf("expected %q, got %q", "0", result)
		}
	})
}

func TestKeyringVaultBackend_Set_Resolve(t *testing.T) {
	keyring.MockInit() // reset mock state
	kb := NewKeyringVaultBackend("test-set-resolve")

	// Set a secret
	if err := kb.Set("providers/anthropic/primary", "sk-test-key"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Resolve it
	val, err := kb.Resolve("vault://providers/anthropic/primary")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if val != "sk-test-key" {
		t.Errorf("Resolve returned %q, want %q", val, "sk-test-key")
	}
}

func TestKeyringVaultBackend_Delete(t *testing.T) {
	keyring.MockInit()
	kb := NewKeyringVaultBackend("test-delete")

	_ = kb.Set("providers/openai/default", "sk-openai-key")

	if err := kb.Delete("providers/openai/default"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := kb.Resolve("vault://providers/openai/default")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestKeyringVaultBackend_List(t *testing.T) {
	keyring.MockInit()
	kb := NewKeyringVaultBackend("test-list")

	_ = kb.Set("providers/anthropic/primary", "key1")
	_ = kb.Set("providers/openai/default", "key2")

	paths, err := kb.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}

	// Verify both paths are present
	found := map[string]bool{}
	for _, p := range paths {
		found[p] = true
	}
	if !found["providers/anthropic/primary"] {
		t.Error("expected providers/anthropic/primary in list")
	}
	if !found["providers/openai/default"] {
		t.Error("expected providers/openai/default in list")
	}
}

func TestKeyringVaultBackend_List_Empty(t *testing.T) {
	keyring.MockInit()
	kb := NewKeyringVaultBackend("test-list-empty")

	paths, err := kb.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestKeyringVaultBackend_Resolve_NotFound(t *testing.T) {
	keyring.MockInit()
	kb := NewKeyringVaultBackend("test-not-found")

	_, err := kb.Resolve("vault://providers/nonexistent/key")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
}

func TestKeyringVaultBackend_Resolve_InvalidRef(t *testing.T) {
	keyring.MockInit()
	kb := NewKeyringVaultBackend("test-invalid-ref")

	_, err := kb.Resolve("not-a-vault-ref")
	if err == nil {
		t.Fatal("expected error for non-vault ref, got nil")
	}
}

func TestKeyringVaultBackend_ConcurrentAccess(t *testing.T) {
	keyring.MockInit()
	kb := NewKeyringVaultBackend("test-concurrent")

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// Concurrent writes
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "providers/concurrent/" + string(rune('a'+i))
			if err := kb.Set(key, "value"); err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent deletes
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "providers/concurrent/" + string(rune('a'+i))
			_ = kb.Delete(key) // may or may not exist yet
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent operation error: %v", err)
	}
}
