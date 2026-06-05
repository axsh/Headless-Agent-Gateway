package codex_test

import (
	"testing"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/codex"
)

func TestCodexAdapterImplementsCodingAgent(t *testing.T) {
	var _ codingagent.CodingAgent = (*codex.CodexAdapter)(nil)
}

func TestCodexAdapterName(t *testing.T) {
	adapter := codex.New(&codingagent.AdapterConfig{})
	if adapter.Name() != "codex" {
		t.Errorf("Name() = %v, want codex", adapter.Name())
	}
}
