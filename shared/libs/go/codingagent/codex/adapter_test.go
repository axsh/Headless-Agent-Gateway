package codex_test

import (
	"testing"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/codingagent/codex"
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
