package codex_test

import (
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
)

func TestBuildEnv_MeteringMeta(t *testing.T) {
	ac := &codingagent.AdapterConfig{GatewayURL: "http://gw:14000"}
	cfg := &codingagent.SessionConfig{TernSessionID: "sess-1", TurnID: "turn-9"}
	env := codex.BuildEnv(ac, cfg)
	var key string
	for _, e := range env {
		if strings.HasPrefix(e, "OPENAI_API_KEY=") {
			key = strings.TrimPrefix(e, "OPENAI_API_KEY=")
		}
	}
	if !strings.Contains(key, ";tern_sid=sess-1") || !strings.Contains(key, ";tid=turn-9") {
		t.Errorf("key = %q, want tern_sid and tid", key)
	}
}
