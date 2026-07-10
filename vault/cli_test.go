package vault

import (
	"bytes"
	"strings"
	"testing"

	sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
	"github.com/zalando/go-keyring"
)

func newTestRunner(t *testing.T, stdin string, stdout, stderr *bytes.Buffer) *CLIRunner {
	t.Helper()
	keyring.MockInit()
	return NewCLIRunner(CLIConfig{
		Store:      sharedvault.NewKeyringVaultBackend("vault-cli-runner-test"),
		Stdin:      strings.NewReader(stdin),
		Stdout:     stdout,
		Stderr:     stderr,
		AppName:    "vault-cli",
		AppVersion: "0.1.0",
	})
}

func TestCLIRunnerSetGetRevealDeleteFlow(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newTestRunner(t, "sk-test-key\n", &out, &errOut)

	if code := runner.Run([]string{"set", "--provider", "anthropic", "--stdin"}); code != 0 {
		t.Fatalf("set code=%d, stderr=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "Set: providers/anthropic/default") {
		t.Fatalf("unexpected set stderr: %q", errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("get code=%d", code)
	}
	if !strings.Contains(out.String(), "providers/anthropic/default: registered") {
		t.Fatalf("unexpected get output: %q", out.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic", "--reveal"}); code != 0 {
		t.Fatalf("get --reveal code=%d", code)
	}
	if out.String() != "sk-test-key" {
		t.Fatalf("unexpected reveal output: %q", out.String())
	}

	errOut.Reset()
	if code := runner.Run([]string{"delete", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("delete code=%d", code)
	}
	if !strings.Contains(errOut.String(), "Deleted: providers/anthropic/default") {
		t.Fatalf("unexpected delete stderr: %q", errOut.String())
	}

	out.Reset()
	if code := runner.Run([]string{"get", "--provider", "anthropic"}); code != 0 {
		t.Fatalf("get after delete code=%d", code)
	}
	if !strings.Contains(out.String(), "not registered") {
		t.Fatalf("expected not registered output, got %q", out.String())
	}
}

func TestCLIRunnerStatusAndList(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newTestRunner(t, "sk-a\n", &out, &errOut)

	if code := runner.Run([]string{"set", "--provider", "anthropic", "--stdin"}); code != 0 {
		t.Fatalf("set anthropic code=%d", code)
	}

	out.Reset()
	if code := runner.Run([]string{"list"}); code != 0 {
		t.Fatalf("list code=%d", code)
	}
	if !strings.Contains(out.String(), "providers/anthropic/default") {
		t.Fatalf("unexpected list output: %q", out.String())
	}

	out.Reset()
	if code := runner.Run([]string{"status"}); code != 0 {
		t.Fatalf("status code=%d", code)
	}
	if !strings.Contains(out.String(), "anthropic: registered") {
		t.Fatalf("missing anthropic registered: %q", out.String())
	}
	if !strings.Contains(out.String(), "openai: not registered") {
		t.Fatalf("missing openai not registered: %q", out.String())
	}
}

func TestCLIRunnerHelpVersionAndInvalidCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := newTestRunner(t, "", &out, &errOut)

	if code := runner.Run([]string{"version"}); code != 0 {
		t.Fatalf("version code=%d", code)
	}
	if !strings.Contains(out.String(), "vault-cli v0.1.0") {
		t.Fatalf("unexpected version output: %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := runner.Run([]string{"help"}); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("help text missing usage: %q", errOut.String())
	}

	errOut.Reset()
	if code := runner.Run([]string{"unknown"}); code != 1 {
		t.Fatalf("invalid command code=%d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "Unknown command: unknown") {
		t.Fatalf("invalid command message missing: %q", errOut.String())
	}
}
