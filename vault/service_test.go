package vault

import (
	"os"
	"strings"
	"testing"

	sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	keyring.MockInit()
	return NewService(sharedvault.NewKeyringVaultBackend("vault-api-test"))
}

func TestResolveKey(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		key      string
		want     string
		wantErr  bool
	}{
		{name: "provider shorthand", provider: "anthropic", want: "providers/anthropic/default"},
		{name: "custom key", key: "custom/path/key", want: "custom/path/key"},
		{name: "missing input", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveKey(tt.provider, tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveKey returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceSetGetDelete(t *testing.T) {
	svc := newTestService(t)

	fullKey, err := svc.Set("anthropic", "", "sk-test-key")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if fullKey != "providers/anthropic/default" {
		t.Fatalf("Set key = %q, want providers/anthropic/default", fullKey)
	}

	res, err := svc.Get("anthropic", "", false)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !res.Registered {
		t.Fatal("expected registered=true")
	}
	if res.Value != "" {
		t.Fatalf("Get without reveal should not include value, got %q", res.Value)
	}

	reveal, err := svc.Get("anthropic", "", true)
	if err != nil {
		t.Fatalf("Get reveal failed: %v", err)
	}
	if reveal.Value != "sk-test-key" {
		t.Fatalf("revealed value = %q, want %q", reveal.Value, "sk-test-key")
	}

	deleted, err := svc.Delete("anthropic", "")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if deleted != "providers/anthropic/default" {
		t.Fatalf("Delete key = %q, want providers/anthropic/default", deleted)
	}

	afterDelete, err := svc.Get("anthropic", "", false)
	if err != nil {
		t.Fatalf("Get after delete failed: %v", err)
	}
	if afterDelete.Registered {
		t.Fatal("expected registered=false after delete")
	}
}

func TestServiceGetRevealNotRegistered(t *testing.T) {
	svc := newTestService(t)

	_, err := svc.Get("openai", "", true)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceListAndStatus(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Set("anthropic", "", "sk-a"); err != nil {
		t.Fatalf("Set anthropic failed: %v", err)
	}
	if _, err := svc.Set("", "providers/openai/default", "sk-o"); err != nil {
		t.Fatalf("Set openai failed: %v", err)
	}

	keys, err := svc.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("List length = %d, want 2", len(keys))
	}

	status, err := svc.Status([]string{"anthropic", "openai", "google"})
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if len(status) != 3 {
		t.Fatalf("Status length = %d, want 3", len(status))
	}
	if !status[0].Registered || !status[1].Registered || status[2].Registered {
		t.Fatalf("unexpected status: %+v", status)
	}
}
