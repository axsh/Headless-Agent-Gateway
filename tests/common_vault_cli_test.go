package llm_test

import (
	"bytes"
	"strings"
	"testing"

	sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
	apivault "github.com/axsh/arctic-tern/vault"
	"github.com/zalando/go-keyring"
)

func newIntegrationRunner(t *testing.T, stdin string, out, errOut *bytes.Buffer) *apivault.CLIRunner {
	t.Helper()
	keyring.MockInit()
	return apivault.NewCLIRunner(apivault.CLIConfig{
		Store:      sharedvault.NewKeyringVaultBackend("vault-cli-integration"),
		Stdin:      strings.NewReader(stdin),
		Stdout:     out,
		Stderr:     errOut,
		AppName:    "vault-cli",
		AppVersion: "0.1.0",
	})
}

func TestVaultCLIFlow_SetGetRevealDelete(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newIntegrationRunner(t, "sk-int-key\n", &out, &errOut)

	if code := runner.Run([]string{"set", "--provider", "anthropic", "--stdin"}); code != 0 {
		t.Fatalf("set failed: code=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "Set: providers/anthropic/default") {
		t.Fatalf("unexpected set message: %q", errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("get failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "providers/anthropic/default: registered") {
		t.Fatalf("unexpected get output: %q", out.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic", "--reveal"}); code != 0 {
		t.Fatalf("get reveal failed: code=%d", code)
	}
	if out.String() != "sk-int-key" {
		t.Fatalf("unexpected reveal output: %q", out.String())
	}

	errOut.Reset()
	if code := runner.Run([]string{"delete", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("delete failed: code=%d", code)
	}
	if !strings.Contains(errOut.String(), "Deleted: providers/anthropic/default") {
		t.Fatalf("unexpected delete message: %q", errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("get after delete failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "providers/anthropic/default: not registered") {
		t.Fatalf("expected not registered output, got %q", out.String())
	}
}

func TestVaultCLIFlow_ListAndStatus(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newIntegrationRunner(t, "sk-int-openai\n", &out, &errOut)

	if code := runner.Run([]string{"set", "--provider", "openai", "--stdin"}); code != 0 {
		t.Fatalf("set openai failed: code=%d stderr=%q", code, errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"list"}); code != 0 {
		t.Fatalf("list failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "providers/openai/default") {
		t.Fatalf("unexpected list output: %q", out.String())
	}

	out.Reset()
	if code := runner.Run([]string{"status"}); code != 0 {
		t.Fatalf("status failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "openai: registered") {
		t.Fatalf("missing openai registered: %q", out.String())
	}
}

func TestVaultCLIFlow_HelpVersionAndInvalidCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newIntegrationRunner(t, "", &out, &errOut)

	if code := runner.Run([]string{"help"}); code != 0 {
		t.Fatalf("help failed: code=%d", code)
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("help output missing usage: %q", errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"version"}); code != 0 {
		t.Fatalf("version failed: code=%d", code)
	}
	if !strings.Contains(out.String(), "vault-cli v0.1.0") {
		t.Fatalf("unexpected version output: %q", out.String())
	}

	errOut.Reset()
	if code := runner.Run([]string{"unknown-command"}); code != 1 {
		t.Fatalf("invalid command code=%d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "Unknown command: unknown-command") {
		t.Fatalf("unexpected invalid command output: %q", errOut.String())
	}
}
