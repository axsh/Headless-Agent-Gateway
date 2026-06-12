package claudecode_test

import (
	"testing"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/codingagent/claudecode"
)

func TestClaudeCodeAdapterImplementsCodingAgent(t *testing.T) {
	var _ codingagent.CodingAgent = (*claudecode.ClaudeCodeAdapter)(nil)
}

func TestClaudeCodeAdapterName(t *testing.T) {
	adapter := claudecode.New(&codingagent.AdapterConfig{})
	if adapter.Name() != "claudecode" {
		t.Errorf("Name() = %v, want claudecode", adapter.Name())
	}
}
