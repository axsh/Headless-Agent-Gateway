package agentservice

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

type embeddingModelsResponse struct {
	Models []llmgateway.ModelInfo `json:"models"`
}

type gatewayErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// handleCreateEmbedding handles POST /api/v1/embeddings.
// Proxies the OpenAI-compatible JSON body to LLMGP POST /v1/embeddings.
// Does not create sessions or start Coding Agents.
func (s *Server) handleCreateEmbedding(w http.ResponseWriter, r *http.Request) {
	if s.logger != nil {
		s.logger.Debug("embeddings request received", "path", r.URL.Path)
	}
	if s.gatewayURL == "" {
		writeEmbeddingJSONError(w, http.StatusServiceUnavailable, "api_error", "LLM gateway not configured", "gateway_not_configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeEmbeddingJSONError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body", "request_read_error")
		return
	}
	defer r.Body.Close()

	gwReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.gatewayURL+"/v1/embeddings", strings.NewReader(string(body)))
	if err != nil {
		writeEmbeddingJSONError(w, http.StatusInternalServerError, "api_error", "failed to create gateway request", "gateway_request_error")
		return
	}
	gwReq.Header.Set("Content-Type", "application/json")
	if s.gatewayToken != "" {
		gwReq.Header.Set("X-Gateway-Token", s.gatewayToken)
	}

	resp, err := http.DefaultClient.Do(gwReq)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("embeddings gateway proxy failed", "error", err.Error(), "gateway_url", s.gatewayURL)
		}
		writeEmbeddingJSONError(w, http.StatusBadGateway, "api_error", "failed to reach LLM gateway", "gateway_unreachable")
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeEmbeddingJSONError(w, http.StatusBadGateway, "api_error", "failed to read gateway response", "gateway_response_error")
		return
	}

	for k, vals := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// handleListEmbeddingModels handles GET /api/v1/embeddings/models.
func (s *Server) handleListEmbeddingModels(w http.ResponseWriter, r *http.Request) {
	models := []llmgateway.ModelInfo{}
	if s.profiles != nil {
		for _, ref := range s.profiles.ListModelRefs(config.ModelModeEmbedding) {
			models = append(models, llmgateway.ModelInfo{
				Provider: ref.Provider,
				Model:    ref.Model,
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(embeddingModelsResponse{Models: models})
}

func (s *Server) routeEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handleCreateEmbedding(w, r)
}

func (s *Server) routeEmbeddingModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handleListEmbeddingModels(w, r)
}

func writeEmbeddingJSONError(w http.ResponseWriter, status int, typ, msg, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var body gatewayErrorBody
	body.Error.Type = typ
	body.Error.Message = msg
	body.Error.Code = code
	json.NewEncoder(w).Encode(body)
}
