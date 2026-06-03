package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/axsh/hag/vault"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func newTestStore() vault.VaultStore {
	keyring.MockInit() // reset for each test
	return vault.NewKeyringVaultBackend("test-cli")
}

func TestResolveKey(t *testing.T) {
	tests := []struct {
		provider string
		key      string
		want     string
	}{
		{provider: "anthropic", want: "providers/anthropic/default"},
		{provider: "openai", want: "providers/openai/default"},
		{key: "custom/path/mykey", want: "custom/path/mykey"},
		{provider: "", key: "", want: ""},
	}
	for _, tt := range tests {
		got := resolveKey(tt.provider, tt.key)
		if got != tt.want {
			t.Errorf("resolveKey(%q, %q) = %q, want %q", tt.provider, tt.key, got, tt.want)
		}
	}
}

func TestRunSetLogic(t *testing.T) {
	store := newTestStore()

	err := runSetLogic(store, setOptions{
		provider: "anthropic",
		value:    "sk-test-key",
	})
	if err != nil {
		t.Fatalf("runSetLogic failed: %v", err)
	}

	// Verify the key was stored
	val, err := store.Resolve("vault://providers/anthropic/default")
	if err != nil {
		t.Fatalf("Resolve after set failed: %v", err)
	}
	if val != "sk-test-key" {
		t.Errorf("got %q, want %q", val, "sk-test-key")
	}
}

func TestRunSetLogic_EmptyValue(t *testing.T) {
	store := newTestStore()

	err := runSetLogic(store, setOptions{
		provider: "anthropic",
		value:    "",
	})
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestRunSetLogic_NoKeyOrProvider(t *testing.T) {
	store := newTestStore()

	err := runSetLogic(store, setOptions{
		value: "some-value",
	})
	if err == nil {
		t.Fatal("expected error when neither provider nor key is specified")
	}
}

func TestRunGetLogic_Registered(t *testing.T) {
	store := newTestStore()
	_ = store.Set("providers/anthropic/default", "sk-key")

	var buf bytes.Buffer
	err := runGetLogic(store, getOptions{provider: "anthropic"}, &buf)
	if err != nil {
		t.Fatalf("runGetLogic failed: %v", err)
	}
	if !strings.Contains(buf.String(), "registered") {
		t.Errorf("output %q does not contain 'registered'", buf.String())
	}
}

func TestRunGetLogic_NotRegistered(t *testing.T) {
	store := newTestStore()

	var buf bytes.Buffer
	err := runGetLogic(store, getOptions{provider: "openai"}, &buf)
	if err != nil {
		t.Fatalf("runGetLogic failed: %v", err)
	}
	if !strings.Contains(buf.String(), "not registered") {
		t.Errorf("output %q does not contain 'not registered'", buf.String())
	}
}

func TestRunGetLogic_Reveal(t *testing.T) {
	store := newTestStore()
	_ = store.Set("providers/anthropic/default", "sk-secret-key")

	var buf bytes.Buffer
	err := runGetLogic(store, getOptions{provider: "anthropic", reveal: true}, &buf)
	if err != nil {
		t.Fatalf("runGetLogic with reveal failed: %v", err)
	}
	if !strings.Contains(buf.String(), "sk-secret-key") {
		t.Errorf("output %q does not contain the secret value", buf.String())
	}
}

func TestRunDeleteLogic(t *testing.T) {
	store := newTestStore()
	_ = store.Set("providers/anthropic/default", "sk-key")

	err := runDeleteLogic(store, deleteOptions{provider: "anthropic"})
	if err != nil {
		t.Fatalf("runDeleteLogic failed: %v", err)
	}

	// Verify key is gone
	_, err = store.Resolve("vault://providers/anthropic/default")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestRunListLogic(t *testing.T) {
	store := newTestStore()
	_ = store.Set("providers/anthropic/default", "key1")
	_ = store.Set("providers/openai/default", "key2")

	var buf bytes.Buffer
	runListLogic(store, &buf)

	output := buf.String()
	if !strings.Contains(output, "providers/anthropic/default") {
		t.Errorf("list output missing anthropic key: %q", output)
	}
	if !strings.Contains(output, "providers/openai/default") {
		t.Errorf("list output missing openai key: %q", output)
	}
}

func TestRunStatusLogic(t *testing.T) {
	store := newTestStore()
	_ = store.Set("providers/anthropic/default", "sk-key")

	var buf bytes.Buffer
	runStatusLogic(store, &buf)

	output := buf.String()
	if !strings.Contains(output, "anthropic: registered") {
		t.Errorf("status output missing 'anthropic: registered': %q", output)
	}
	if !strings.Contains(output, "openai: not registered") {
		t.Errorf("status output missing 'openai: not registered': %q", output)
	}
	if !strings.Contains(output, "google: not registered") {
		t.Errorf("status output missing 'google: not registered': %q", output)
	}
}
