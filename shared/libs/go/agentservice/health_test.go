package agentservice_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/agentservice"
)

func TestHealthHandler_AllHealthy(t *testing.T) {
	// Mock LLMGP server
	mockGW := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer mockGW.Close()

	srv := agentservice.New(agentservice.WithGatewayURL(mockGW.URL))
	handler := srv.HTTPHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp agentservice.HealthResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "ok" {
		t.Errorf("resp.Status = %v, want ok", resp.Status)
	}
	if resp.Gateway.Status != "ok" {
		t.Errorf("gateway.Status = %v, want ok", resp.Gateway.Status)
	}
}

func TestHealthHandler_GatewayDown(t *testing.T) {
	// Use a URL that will refuse connections
	srv := agentservice.New(agentservice.WithGatewayURL("http://127.0.0.1:1"))
	handler := srv.HTTPHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}

	var resp agentservice.HealthResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "degraded" {
		t.Errorf("resp.Status = %v, want degraded", resp.Status)
	}
	if resp.Gateway.Status != "unreachable" {
		t.Errorf("gateway.Status = %v, want unreachable", resp.Gateway.Status)
	}
	if resp.Gateway.Error == "" {
		t.Error("gateway.Error should contain error message")
	}
}

func TestHealthHandler_NoGateway(t *testing.T) {
	// No gateway URL (in-process mode)
	srv := agentservice.New()
	handler := srv.HTTPHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp agentservice.HealthResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Gateway.Status != "ok" {
		t.Errorf("gateway.Status = %v, want ok (in-process)", resp.Gateway.Status)
	}
}
