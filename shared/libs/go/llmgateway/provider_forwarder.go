package llmgateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/logger"
)

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
	log logger.Logger,
) (*http.Response, error) {
	p, ok := GetProvider(provider)
	if !ok {
		return nil, &GatewayError{
			Type:    "api_error",
			Message: "unsupported provider: " + provider,
			Code:    "unsupported_provider",
			Status:  http.StatusBadRequest,
		}
	}

	upstreamURL := p.BaseURL() + path

	req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Delegate provider-specific auth headers to the Provider implementation.
	p.SetAuthHeaders(req, apiKey, originalHeaders)

	if log != nil {
		maskedHeaders := make(http.Header)
		for k, v := range req.Header {
			lowerK := strings.ToLower(k)
			if lowerK == "authorization" || lowerK == "x-api-key" || lowerK == "x-goog-api-key" {
				maskedHeaders.Set(k, "[MASKED]")
			} else {
				maskedHeaders[k] = v
			}
		}
		log.Trace("upstream request", "url", upstreamURL, "headers", fmt.Sprintf("%+v", maskedHeaders))
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

// RetryConfig configures retry behavior for upstream provider requests.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts (0 = no retry).
	MaxRetries int
	// InitialDelay is the base delay for exponential backoff.
	InitialDelay time.Duration
	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
	}
}

// isRetryableStatusCode returns true for HTTP status codes that warrant a retry.
func isRetryableStatusCode(status int) bool {
	switch status {
	case http.StatusTooManyRequests,    // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

// isRetryableNetworkError returns true for transient network errors.
func isRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connectex")
}

// calculateBackoff computes the backoff delay for a given attempt.
// If resp has a Retry-After header (for 429), it is used instead.
func calculateBackoff(attempt int, cfg *RetryConfig, resp *http.Response) time.Duration {
	// Check Retry-After header for 429 responses.
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, err := strconv.Atoi(ra); err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	// Exponential backoff: InitialDelay * 2^attempt
	delay := cfg.InitialDelay * (1 << uint(attempt))
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	// Add jitter: +/- 10%
	jitter := time.Duration(rand.Int63n(int64(delay) / 5))
	return delay - time.Duration(int64(delay)/10) + jitter
}

// forwardWithRetry sends a request to the upstream provider with retry logic.
// It retries on retryable status codes (429, 5xx) and network errors.
// Client errors (4xx except 429) are returned immediately without retry.
func (f *providerForwarder) forwardWithRetry(
	ctx context.Context,
	provider, path string,
	body []byte,
	apiKey string,
	headers http.Header,
	cfg *RetryConfig,
	log logger.Logger,
) (*http.Response, error) {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}
	if log != nil {
		log.Debug("forwarding request to upstream", "provider", provider, "path", path)
	}
	var lastErr error
	var lastResp *http.Response
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp, err := f.forwardToProvider(provider, path, body, apiKey, headers, log)
		if err == nil && resp != nil && resp.Body != nil {
			if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
				if log != nil {
					log.Trace("upstream response body preview", "body_preview", "[SSE Stream]")
				}
			} else {
				previewBuf := make([]byte, 1024)
				n, _ := resp.Body.Read(previewBuf)
				if n > 0 {
					if log != nil {
						log.Trace("upstream response body preview", "body_preview", string(previewBuf[:n]))
					}
					resp.Body = struct {
						io.Reader
						io.Closer
					}{
						Reader: io.MultiReader(bytes.NewReader(previewBuf[:n]), resp.Body),
						Closer: resp.Body,
					}
				}
			}
		}

		if err != nil {
			if !isRetryableNetworkError(err) {
				return nil, err
			}
			lastErr = err
			if log != nil {
				delay := calculateBackoff(attempt, cfg, nil)
				log.Warn("retrying upstream request",
					"attempt", attempt+1,
					"max_retries", cfg.MaxRetries,
					"delay_ms", delay.Milliseconds(),
					"status", 0,
					"error", err.Error())
			}
		} else if !isRetryableStatusCode(resp.StatusCode) {
			// Non-retryable status (200, 400, 401, etc.) - return immediately.
			return resp, nil
		} else {
			// Retryable status (429, 5xx) - drain body and retry.
			lastResp = resp
			delay := calculateBackoff(attempt, cfg, lastResp)
			if log != nil {
				log.Warn("retrying upstream request",
					"attempt", attempt+1,
					"max_retries", cfg.MaxRetries,
					"delay_ms", delay.Milliseconds(),
					"status", resp.StatusCode,
					"error", "")
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = &GatewayError{
				Type:    "api_error",
				Message: fmt.Sprintf("upstream returned %d", resp.StatusCode),
				Code:    "upstream_retryable",
				Status:  resp.StatusCode,
			}
		}

		// Wait before next attempt (except on last attempt).
		if attempt < cfg.MaxRetries {
			delay := calculateBackoff(attempt, cfg, lastResp)
			if log != nil {
				log.Info("retry backoff",
					"attempt", attempt+1,
					"delay", delay)
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if log != nil {
		errStr := ""
		if lastErr != nil {
			errStr = lastErr.Error()
		}
		log.Error("all retries exhausted",
			"attempts", cfg.MaxRetries+1,
			"last_error", errStr,
			"provider", provider,
			"path", path)
	}
	return nil, lastErr
}

// urlOverrideProvider wraps an existing Provider, overriding only the base URL.
// This preserves the original provider's SetAuthHeaders and BifrostProvider behavior.
type urlOverrideProvider struct {
	Provider // embedded original provider
	baseURL  string
}

func (p *urlOverrideProvider) BaseURL() string { return p.baseURL }

// overrideProviderBaseURL temporarily overrides a provider's base URL for testing.
// Returns a cleanup function that restores the original provider.
func overrideProviderBaseURL(provider, url string) func() {
	providerMu.Lock()
	orig := providerRegistry[provider]
	if orig != nil {
		providerRegistry[provider] = &urlOverrideProvider{Provider: orig, baseURL: url}
	}
	providerMu.Unlock()

	return func() {
		providerMu.Lock()
		if orig != nil {
			providerRegistry[provider] = orig
		} else {
			delete(providerRegistry, provider)
		}
		providerMu.Unlock()
	}
}
