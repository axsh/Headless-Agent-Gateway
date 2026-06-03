package llmgateway

import "testing"

// Compile-time check: StubGateway implements LLMGatewayBackend.
var _ LLMGatewayBackend = (*StubGateway)(nil)

func TestStubGateway_Lifecycle(t *testing.T) {
	stub := NewStubGateway()

	if stub.Launched {
		t.Fatal("expected Launched to be false before Launch()")
	}
	if stub.ShutDown {
		t.Fatal("expected ShutDown to be false before Shutdown()")
	}

	if err := stub.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !stub.Launched {
		t.Fatal("expected Launched to be true after Launch()")
	}

	if err := stub.Shutdown(nil); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !stub.ShutDown {
		t.Fatal("expected ShutDown to be true after Shutdown()")
	}
}

func TestStubGateway_ProxyURL(t *testing.T) {
	stub := NewStubGateway()
	if got := stub.ProxyURL(); got != "" {
		t.Errorf("ProxyURL() = %q, want empty string", got)
	}
}

func TestStubGateway_ListModels(t *testing.T) {
	stub := NewStubGateway()
	models := stub.ListModels()
	if models != nil {
		t.Errorf("ListModels() = %v, want nil", models)
	}
}

func TestStubGateway_Health(t *testing.T) {
	stub := NewStubGateway()
	health := stub.Health()
	if health.Status != "stub" {
		t.Errorf("Health().Status = %q, want %q", health.Status, "stub")
	}
}
