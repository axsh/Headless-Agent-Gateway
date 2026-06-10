package llmgateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsRetryableStatusCode(t *testing.T) {
	tests := []struct {
		status    int
		retryable bool
	}{
		{200, false},
		{201, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			got := isRetryableStatusCode(tt.status)
			if got != tt.retryable {
				t.Errorf("isRetryableStatusCode(%d) = %v, want %v", tt.status, got, tt.retryable)
			}
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	cfg := &RetryConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     10 * time.Second,
	}

	tests := []struct {
		attempt  int
		minDelay time.Duration
		maxDelay time.Duration
	}{
		{0, 900 * time.Millisecond, 1100 * time.Millisecond},  // ~1s +/- 10%
		{1, 1800 * time.Millisecond, 2200 * time.Millisecond}, // ~2s +/- 10%
		{2, 3600 * time.Millisecond, 4400 * time.Millisecond}, // ~4s +/- 10%
		{3, 7200 * time.Millisecond, 8800 * time.Millisecond}, // ~8s +/- 10% (capped to MaxDelay=10s)
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			delay := calculateBackoff(tt.attempt, cfg, nil)
			if delay < tt.minDelay || delay > tt.maxDelay {
				t.Errorf("calculateBackoff(%d) = %v, want between %v and %v", tt.attempt, delay, tt.minDelay, tt.maxDelay)
			}
		})
	}
}

func TestCalculateBackoff_RetryAfterHeader(t *testing.T) {
	cfg := &RetryConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
	}
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"5"}},
	}
	delay := calculateBackoff(0, cfg, resp)
	if delay != 5*time.Second {
		t.Errorf("calculateBackoff with Retry-After = %v, want 5s", delay)
	}
}

func TestForwardWithRetry_Success(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{
		client: srv.Client(),
	}
	// Override base URL for test.
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &RetryConfig{MaxRetries: 3, InitialDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	resp, err := fwd.forwardWithRetry(context.Background(), "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

func TestForwardWithRetry_429(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&callCount, 1)
		if c == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{client: srv.Client()}
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &RetryConfig{MaxRetries: 3, InitialDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	resp, err := fwd.forwardWithRetry(context.Background(), "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}

func TestForwardWithRetry_500(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&callCount, 1)
		if c == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{client: srv.Client()}
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &RetryConfig{MaxRetries: 3, InitialDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	resp, err := fwd.forwardWithRetry(context.Background(), "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}

func TestForwardWithRetry_400_NoRetry(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad_request"}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{client: srv.Client()}
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &RetryConfig{MaxRetries: 3, InitialDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	resp, err := fwd.forwardWithRetry(context.Background(), "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// 400 is non-retryable, should be returned directly.
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("callCount = %d, want 1 (no retry for 400)", callCount)
	}
}

func TestForwardWithRetry_MaxAttempts(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{client: srv.Client()}
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &RetryConfig{MaxRetries: 2, InitialDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	resp, err := fwd.forwardWithRetry(context.Background(), "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected error after max retries, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want containing '500'", err)
	}
	// 1 initial + 2 retries = 3 total
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
}

func TestForwardWithRetry_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{client: srv.Client()}
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	cfg := &RetryConfig{MaxRetries: 3, InitialDelay: 1 * time.Second, MaxDelay: 5 * time.Second}
	_, err := fwd.forwardWithRetry(ctx, "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestForwardWithRetry_RetryAfterHeader(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&callCount, 1)
		if c == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate_limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	fwd := &providerForwarder{client: srv.Client()}
	origURL := providerBaseURLs["openai"]
	providerBaseURLs["openai"] = srv.URL
	defer func() { providerBaseURLs["openai"] = origURL }()

	cfg := &RetryConfig{MaxRetries: 3, InitialDelay: 10 * time.Millisecond, MaxDelay: 50 * time.Millisecond}
	start := time.Now()
	resp, err := fwd.forwardWithRetry(context.Background(), "openai", "/v1/test", []byte(`{}`), "key", nil, cfg, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// Retry-After: 1 should cause at least 1 second of delay.
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 900ms (Retry-After: 1)", elapsed)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "ok") {
		t.Errorf("body = %s, want containing 'ok'", body)
	}
}
