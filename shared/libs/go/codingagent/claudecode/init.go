package claudecode

import (
	"os/exec"

	"github.com/axsh/arctic-tern/codingagent"
)

func init() {
	codingagent.Register("claudecode", func(cfg *codingagent.AdapterConfig) (codingagent.CodingAgent, error) {
		if _, err := exec.LookPath("claude"); err != nil {
			return nil, nil // CLI not available, skip
		}
		return New(cfg), nil
	})
}
