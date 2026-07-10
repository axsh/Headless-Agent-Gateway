package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
	apivault "github.com/axsh/arctic-tern/vault"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func newTestStore() sharedvault.VaultStore {
	keyring.MockInit() // reset for each test
	return sharedvault.NewKeyringVaultBackend("test-cli")
}

func newRunner(t *testing.T, stdin string, out, errOut *bytes.Buffer) *apivault.CLIRunner {
	t.Helper()
	return apivault.NewCLIRunner(apivault.CLIConfig{
		Store:      newTestStore(),
		Stdin:      strings.NewReader(stdin),
		Stdout:     out,
		Stderr:     errOut,
		AppName:    "vault-cli",
		AppVersion: "0.1.0",
	})
}

func TestVaultCLIAsSample_SetGetDeleteFlow(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newRunner(t, "sk-key\n", &out, &errOut)

	if code := runner.Run([]string{"set", "--provider", "anthropic", "--stdin"}); code != 0 {
		t.Fatalf("set failed: code=%d stderr=%q", code, errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("get failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "providers/anthropic/default: registered") {
		t.Fatalf("unexpected get output: %q", out.String())
	}

	out.Reset()
	if code := runner.Run([]string{"delete", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("delete failed: code=%d", code)
	}
	if code := runner.Run([]string{"get", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("get after delete failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "not registered") {
		t.Fatalf("expected not registered output, got %q", out.String())
	}
}

func TestVaultCLIAsSample_Status(t *testing.T) {
	var buf bytes.Buffer
	var errOut bytes.Buffer
	runner := newRunner(t, "sk-key\n", &buf, &errOut)

	if code := runner.Run([]string{"set", "--provider", "anthropic", "--stdin"}); code != 0 {
		t.Fatalf("set failed: code=%d", code)
	}

	buf.Reset()
	if code := runner.Run([]string{"status"}); code != 0 {
		t.Fatalf("status failed: code=%d", code)
	}

	output := buf.String()
	if !strings.Contains(output, "anthropic: registered") {
		t.Fatalf("status output missing 'anthropic: registered': %q", output)
	}
	if !strings.Contains(output, "openai: not registered") {
		t.Fatalf("status output missing 'openai: not registered': %q", output)
	}
	if !strings.Contains(output, "google: not registered") {
		t.Fatalf("status output missing 'google: not registered': %q", output)
	}
}
