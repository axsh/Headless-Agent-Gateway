package codex

import (
	"os/exec"

	"github.com/axsh/arctic-tern/codingagent"
)

func init() {
	codingagent.Register("codex", func(cfg *codingagent.AdapterConfig) (codingagent.CodingAgent, error) {
		if _, err := exec.LookPath("codex"); err != nil {
			return nil, nil // CLI not available, skip
		}
		return New(cfg), nil
	})
}
