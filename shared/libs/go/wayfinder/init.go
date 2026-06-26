package wayfinder

import "github.com/axsh/arctic-tern/shared/libs/go/codingagent"

func init() {
	codingagent.Register(AgentName, func(cfg *codingagent.AdapterConfig) (codingagent.CodingAgent, error) {
		// Wayfinder is a Go library agent; always available (no CLI dependency).
		return NewAdapter(cfg), nil
	})
}
