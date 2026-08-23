package codingagent

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	ErrorCodeUpstreamOverloaded = "upstream_overloaded"
	ErrorCodeUpstreamError      = "upstream_error"
)

const (
	// DefaultMaxAttempts is the default maximum number of retry attempts.
	DefaultMaxAttempts = 10
	// DefaultRetryInterval is the default interval between retry attempts.
	DefaultRetryInterval = 3 * time.Second
)

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts   int
	RetryInterval time.Duration
	// ContainerCheck is an optional container liveness check function.
	// If nil, no check is performed.
	// If it returns false, retry is immediately aborted.
	ContainerCheck func() bool
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:   DefaultMaxAttempts,
		RetryInterval: DefaultRetryInterval,
	}
}

// IsRetryableUpstream reports whether a log, stderr, or API message is a
// transient upstream stream failure.
func IsRetryableUpstream(msg string) bool {
	if msg == "" {
		return false
	}
	if IsRetryableError(errors.New(msg)) {
		return true
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "reconnecting...") ||
		strings.Contains(lower, "we're currently experiencing high demand") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "429")
}

// IsNonRetryableError reports a fatal Codex CLI / auth / argv failure
// that must not trigger process re-exec.
func IsNonRetryableError(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	needles := []string{
		"unauthorized",
		"invalid api key",
		"invalid_api_key",
		"authentication failed",
		"model not found",
		"unknown model",
		"invalid argument",
		"flag provided but not defined",
		"failed to resolve api key from vault",
		"vault_error",
		"unexpected status 404",
		"unexpected status 401",
		"unexpected status 403",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// IsSandboxRejection reports Codex sandbox/policy rejection in stderr or log text.
func IsSandboxRejection(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "rejected(") {
		return true
	}
	if strings.Contains(lower, "rm -f style commands are not permitted") {
		return true
	}
	if strings.Contains(lower, "exec_command failed") && strings.Contains(lower, "rejected") {
		return true
	}
	if strings.Contains(lower, "blocked by policy") {
		return true
	}
	if strings.Contains(lower, "rejected by the environment policy") {
		return true
	}
	return false
}

// ClassifiedErrorContent appends a stable error code tag for SSE EventError content.
func ClassifiedErrorContent(msg string, retryable bool) string {
	code := ErrorCodeUpstreamError
	if retryable {
		code = ErrorCodeUpstreamOverloaded
	}
	tag := "[" + code + "]"
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return tag
	}
	if strings.Contains(msg, tag) {
		return msg
	}
	return msg + " " + tag
}

// IsRetryableError checks whether the error is retryable.
// EOF, connection reset, broken pipe, connection refused, connectex are retryable.
func IsRetryableError(err error) bool {
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

// Retry executes fn up to MaxAttempts times.
// Only retryable errors trigger a retry.
// If ContainerCheck returns false, retry is immediately aborted.
func Retry(ctx context.Context, cfg *RetryConfig, fn func() error) error {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}
	var lastErr error
	for attempt := range cfg.MaxAttempts {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !IsRetryableError(lastErr) {
			return lastErr
		}
		if cfg.ContainerCheck != nil && !cfg.ContainerCheck() {
			return lastErr
		}

		if attempt < cfg.MaxAttempts-1 {
			select {
			case <-time.After(cfg.RetryInterval):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return lastErr
}
