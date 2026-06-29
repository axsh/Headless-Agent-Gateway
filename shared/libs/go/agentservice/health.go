package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const healthCheckTimeout = 2 * time.Second

// HealthResponse is the health check response structure.
type HealthResponse struct {
	Status         string            `json:"status"`
	CLIVersions    map[string]string `json:"cli_versions"`
	Gateway        GatewayHealth     `json:"gateway"`
	ServerSettings ServerSettings    `json:"server_settings"`
}

// GatewayHealth is the health status of the LLM Gateway Proxy.
type GatewayHealth struct {
	Status        string    `json:"status"`
	URL           string    `json:"url"`
	Error         string    `json:"error,omitempty"`
	LastCheckedAt time.Time `json:"last_checked_at"`
}

// ServerSettings contains the server configurations.
type ServerSettings struct {
	DisableSandbox  bool  `json:"disable_sandbox"`
	EnableSubagent  bool  `json:"enable_subagent"`
	EnabledVersions []int `json:"enabled_versions"`
}

// handleHealth handles GET /health.
// Performs a cascading health check by returning the cached LLMGP status.
// 200: CAWA + LLMGP healthy.
// 502: CAWA healthy, LLMGP unreachable.
// No response: CAWA down.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.gatewayHealthMu.Lock()
	gwHealth := s.lastGatewayHealth
	s.gatewayHealthMu.Unlock()

	// Construct list of enabled versions
	versions := make([]int, 0, len(s.enabledVersions))
	for v := range s.enabledVersions {
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		versions = []int{1}
	}

	resp := HealthResponse{
		Status:      "ok",
		CLIVersions: s.cliVersions,
		Gateway:     gwHealth,
		ServerSettings: ServerSettings{
			DisableSandbox:  s.disableSandbox,
			EnableSubagent:  s.enableSubagent,
			EnabledVersions: versions,
		},
	}

	w.Header().Set("Content-Type", "application/json")

	if gwHealth.Status != "ok" {
		resp.Status = "degraded"
		w.WriteHeader(http.StatusBadGateway) // 502
	} else {
		w.WriteHeader(http.StatusOK) // 200
	}

	json.NewEncoder(w).Encode(resp)
}

// startGatewayHealthPolling polls the LLM Gateway Proxy health status periodically.
func (s *Server) startGatewayHealthPolling(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial check on launch
	s.updateGatewayHealth()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.updateGatewayHealth()
		}
	}
}

func (s *Server) updateGatewayHealth() {
	gwHealth := s.checkGatewayHealth()
	gwHealth.LastCheckedAt = time.Now()

	s.gatewayHealthMu.Lock()
	s.lastGatewayHealth = gwHealth
	s.gatewayHealthMu.Unlock()
}

// checkGatewayHealth calls LLMGP /health endpoint.
func (s *Server) checkGatewayHealth() GatewayHealth {
	if s.gatewayURL == "" {
		return GatewayHealth{Status: "ok", URL: "(in-process)"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", s.gatewayURL+"/health", nil)
	if err != nil {
		return GatewayHealth{
			Status: "unreachable", URL: s.gatewayURL, Error: err.Error(),
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return GatewayHealth{
			Status: "unreachable", URL: s.gatewayURL, Error: err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return GatewayHealth{
			Status: "unhealthy", URL: s.gatewayURL,
			Error: "HTTP " + resp.Status,
		}
	}
	return GatewayHealth{Status: "ok", URL: s.gatewayURL}
}
