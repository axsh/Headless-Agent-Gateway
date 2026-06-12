package client

import (
	"net/http"
	"testing"
	"time"
)

func TestNew_DefaultConfig(t *testing.T) {
	c := New("http://localhost:3100")

	if c.baseURL != "http://localhost:3100" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "http://localhost:3100")
	}
	if c.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, 30*time.Second)
	}
}

func TestNew_WithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 60 * time.Second}
	c := New("http://localhost:3100", WithHTTPClient(custom))

	if c.httpClient != custom {
		t.Fatal("httpClient was not set to custom client")
	}
	if c.httpClient.Timeout != 60*time.Second {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, 60*time.Second)
	}
}
