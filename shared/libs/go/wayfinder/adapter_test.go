package wayfinder

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestAdapter_ImplementsCodingAgent(t *testing.T) {
	var _ codingagent.CodingAgent = (*Adapter)(nil)
}

func TestAdapter_Name(t *testing.T) {
	adapter := NewAdapter(&codingagent.AdapterConfig{
		GatewayURL: "http://127.0.0.1:8080", GatewayToken: "token",
	})
	if adapter.Name() != "wayfinder" {
		t.Errorf("Name = %q, want %q", adapter.Name(), "wayfinder")
	}
}

func TestAdapter_Close(t *testing.T) {
	adapter := NewAdapter(&codingagent.AdapterConfig{
		GatewayURL: "http://127.0.0.1:8080", GatewayToken: "token",
	})
	if err := adapter.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestWayfinderSession_ImplementsSession(t *testing.T) {
	var _ codingagent.Session = (*wayfinderSession)(nil)
}
