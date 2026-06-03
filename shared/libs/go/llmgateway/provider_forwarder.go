package llmgateway

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"
)

// providerBaseURLs maps provider names to their API base URLs.
var providerBaseURLs = map[string]string{
	"anthropic": "https://api.anthropic.com",
	"openai":    "https://api.openai.com",
	"google":    "https://generativelanguage.googleapis.com",
}

// providerForwarder handles forwarding requests to upstream LLM providers.
type providerForwarder struct {
	client *http.Client
}

// newProviderForwarder creates a forwarder with a configured HTTP client.
func newProviderForwarder() *providerForwarder {
	return &providerForwarder{
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// forwardToProvider sends a request body to the upstream provider API.
// path is the API path (e.g., "/v1/messages").
// Returns the upstream response, which the caller is responsible for closing.
func (f *providerForwarder) forwardToProvider(
	provider string,
	path string,
	body []byte,
	apiKey string,
	originalHeaders http.Header,
) (*http.Response, error) {
	baseURL, ok := providerBaseURLs[provider]
	if !ok {
		return nil, &GatewayError{
			Type:    "api_error",
			Message: "unsupported provider: " + provider,
			Code:    "unsupported_provider",
			Status:  http.StatusBadRequest,
		}
	}

	upstreamURL := baseURL + path

	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Set provider-specific auth headers
	switch provider {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		// Forward anthropic-beta if present
		if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
			req.Header.Set("anthropic-beta", beta)
		}
	case "openai":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "google":
		// Google uses API key as query parameter
		req.URL.RawQuery = "key=" + apiKey
	}

	return f.client.Do(req)
}

// proxyResponse copies the upstream response to the downstream client.
// It detects and handles text/event-stream streaming response natively by flushing chunks.
func proxyResponse(w http.ResponseWriter, resp *http.Response) {
	// Copy relevant headers
	for key, vals := range resp.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream text/event-stream data immediately
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		flusher, ok := w.(http.Flusher)
		if ok {
			buf := make([]byte, 4096)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					flusher.Flush()
				}
				if err != nil {
					break
				}
			}
			return
		}
	}

	io.Copy(w, resp.Body)
}
