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
	Status      string            `json:"status"`
	Agents      []string          `json:"agents"`
	CLIVersions map[string]string `json:"cli_versions"`
	Gateway     GatewayHealth     `json:"gateway"`
}

// GatewayHealth is the health status of the LLM Gateway Proxy.
type GatewayHealth struct {
	Status string `json:"status"`
	URL    string `json:"url"`
	Error  string `json:"error,omitempty"`
}

// handleHealth handles GET /health.
// Performs a cascading health check: CAWA -> LLMGP.
// 200: CAWA + LLMGP healthy.
// 502: CAWA healthy, LLMGP unreachable.
// No response: CAWA down.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	agentNames := make([]string, 0, len(s.agents))
	for name := range s.agents {
		agentNames = append(agentNames, name)
	}

	gwHealth := s.checkGatewayHealth()

	resp := HealthResponse{
		Status:      "ok",
		Agents:      agentNames,
		CLIVersions: s.cliVersions,
		Gateway:     gwHealth,
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
