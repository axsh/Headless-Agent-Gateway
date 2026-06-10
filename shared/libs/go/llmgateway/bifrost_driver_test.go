package llmgateway

import (
	"context"
	"testing"

	"github.com/axsh/hag/config"
)

func TestBifrostDriver_New(t *testing.T) {
	t.Run("creates with valid profiles", func(t *testing.T) {
		cfg := &config.AppConfig{}
		profiles := testProfiles()
		driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if driver == nil {
			t.Fatal("expected non-nil driver")
		}
	})

	t.Run("creates with nil profiles", func(t *testing.T) {
		cfg := &config.AppConfig{}
		driver, err := NewBifrostDriver(cfg, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if driver == nil {
			t.Fatal("expected non-nil driver")
		}
	})
}

func TestBifrostDriver_ListModels(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	models := driver.ListModels()
	if len(models) == 0 {
		t.Fatal("expected at least one model")
	}

	// Verify at least one anthropic and one openai model
	var hasAnthropic, hasOpenAI bool
	for _, m := range models {
		if m.Provider == "anthropic" {
			hasAnthropic = true
		}
		if m.Provider == "openai" {
			hasOpenAI = true
		}
	}
	if !hasAnthropic {
		t.Error("expected anthropic model in list")
	}
	if !hasOpenAI {
		t.Error("expected openai model in list")
	}
}

func TestBifrostDriver_Health(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	health := driver.Health()
	if health.Status != "ok" {
		t.Errorf("health status = %q, want %q", health.Status, "ok")
	}
	if health.Models == 0 {
		t.Error("expected non-zero model count")
	}
}

func TestBifrostDriver_LaunchShutdown(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	if err := driver.Launch(ctx); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	url := driver.ProxyURL()
	if url == "" {
		t.Fatal("ProxyURL should not be empty after Launch")
	}
	t.Logf("proxy URL: %s", url)

	if err := driver.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}

func TestBifrostDriver_ProxyURL_BeforeLaunch(t *testing.T) {
	cfg := &config.AppConfig{}
	driver, err := NewBifrostDriver(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Before Launch, ProxyURL should return an address with port 0
	url := driver.ProxyURL()
	if url == "" {
		t.Fatal("ProxyURL should not be empty even before Launch")
	}
}

func TestBifrostDriver_BifrostSDK_Init(t *testing.T) {
	t.Run("initializes with valid profiles", func(t *testing.T) {
		cfg := &config.AppConfig{}
		profiles := testProfiles()
		driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if driver.bifrostSDK == nil {
			t.Fatal("expected bifrostSDK to be initialized with valid profiles")
		}
		// Clean up
		driver.bifrostSDK.Shutdown()
	})

	t.Run("initializes with nil profiles gracefully", func(t *testing.T) {
		cfg := &config.AppConfig{}
		driver, err := NewBifrostDriver(cfg, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// With nil profiles, SDK may or may not init successfully
		// but driver creation should not fail.
		if driver.bifrostSDK != nil {
			driver.bifrostSDK.Shutdown()
		}
	})

	t.Run("shutdown does not panic", func(t *testing.T) {
		cfg := &config.AppConfig{}
		profiles := testProfiles()
		driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should not panic even with Bifrost SDK initialized.
		ctx := context.Background()
		if err := driver.Launch(ctx); err != nil {
			t.Fatalf("Launch failed: %v", err)
		}
		if err := driver.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown failed: %v", err)
		}
	})
}

func TestBifrostDriver_BifrostSDK_ReloadProfiles(t *testing.T) {
	cfg := &config.AppConfig{}
	profiles := testProfiles()
	driver, err := NewBifrostDriver(cfg, profiles, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	originalSDK := driver.bifrostSDK
	if originalSDK == nil {
		t.Fatal("expected bifrostSDK to be initialized before reload")
	}

	// Reload with new profiles — should reinitialize the SDK.
	newProfiles := testProfiles()
	driver.ReloadProfiles(newProfiles)

	if driver.bifrostSDK == nil {
		t.Fatal("expected bifrostSDK to be reinitialized after reload")
	}
	if driver.bifrostSDK == originalSDK {
		t.Error("expected bifrostSDK to be a new instance after reload")
	}

	// Clean up
	driver.bifrostSDK.Shutdown()
}

