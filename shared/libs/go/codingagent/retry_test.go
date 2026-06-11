package codingagent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/codingagent"
)

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{errors.New("EOF"), true},
		{errors.New("read: connection reset by peer"), true},
		{errors.New("write: broken pipe"), true},
		{errors.New("dial tcp: connection refused"), true},
		{errors.New("connectex: no connection"), true},
		{errors.New("404 not found"), false},
		{errors.New("unauthorized"), false},
		{nil, false},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.err != nil {
			name = tt.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			got := codingagent.IsRetryableError(tt.err)
			if got != tt.expected {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestRetryWithSuccess(t *testing.T) {
	callCount := 0
	cfg := &codingagent.RetryConfig{
		MaxAttempts:   5,
		RetryInterval: 1 * time.Millisecond,
	}

	err := codingagent.Retry(context.Background(), cfg, func() error {
		callCount++
		if callCount < 3 {
			return errors.New("connection refused")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Retry returned error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("callCount = %d, want 3", callCount)
	}
}

func TestRetryAllFail(t *testing.T) {
	cfg := &codingagent.RetryConfig{
		MaxAttempts:   3,
		RetryInterval: 1 * time.Millisecond,
	}

	err := codingagent.Retry(context.Background(), cfg, func() error {
		return errors.New("connection refused")
	})

	if err == nil {
		t.Error("Retry should return error when all attempts fail")
	}
}

func TestRetryNonRetryableError(t *testing.T) {
	callCount := 0
	cfg := &codingagent.RetryConfig{
		MaxAttempts:   5,
		RetryInterval: 1 * time.Millisecond,
	}

	err := codingagent.Retry(context.Background(), cfg, func() error {
		callCount++
		return errors.New("unauthorized")
	})

	if err == nil || err.Error() != "unauthorized" {
		t.Errorf("Retry error = %v, want unauthorized", err)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry for non-retryable)", callCount)
	}
}

func TestRetryWithContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := &codingagent.RetryConfig{
		MaxAttempts:   5,
		RetryInterval: 1 * time.Millisecond,
	}

	err := codingagent.Retry(ctx, cfg, func() error {
		return errors.New("connection refused")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Retry error = %v, want context.Canceled", err)
	}
}

func TestRetryContainerCheckAbort(t *testing.T) {
	callCount := 0
	cfg := &codingagent.RetryConfig{
		MaxAttempts:   5,
		RetryInterval: 1 * time.Millisecond,
		ContainerCheck: func() bool {
			return false // container is dead
		},
	}

	err := codingagent.Retry(context.Background(), cfg, func() error {
		callCount++
		return errors.New("connection refused")
	})

	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (should abort on dead container)", callCount)
	}
	if err == nil {
		t.Error("Retry should return error when container is dead")
	}
}
