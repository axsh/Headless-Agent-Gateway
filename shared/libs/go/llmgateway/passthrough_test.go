package llmgateway

import (
	"context"
	"testing"
	"time"
)

func TestPassthroughDriver_Lifecycle(t *testing.T) {
	driver := NewPassthroughDriver(0) // auto port

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := driver.Launch(ctx)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	url := driver.ProxyURL()
	if url == "" {
		t.Errorf("expected ProxyURL to be set")
	}

	// Verify health
	health := driver.Health()
	if health.Status != "ok" {
		t.Errorf("expected health status 'ok', got %q", health.Status)
	}

	// Shutdown
	err = driver.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}
