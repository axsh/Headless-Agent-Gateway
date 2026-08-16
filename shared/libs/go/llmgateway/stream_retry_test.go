package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

func TestOpenStreamWithRetry_RetriesRetryableStartError(t *testing.T) {
	calls := 0
	out, err := OpenStreamWithRetry(context.Background(), config.RetrySettings{
		MaxRetries: 2,
	}, nil, func() (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("Reconnecting... 1/5 (We're currently experiencing high demand)")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("OpenStreamWithRetry: %v", err)
	}
	if out != 42 {
		t.Errorf("out = %d, want 42", out)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestOpenStreamWithRetry_NonRetryableNoRetry(t *testing.T) {
	calls := 0
	_, err := OpenStreamWithRetry(context.Background(), config.RetrySettings{
		MaxRetries: 2,
	}, nil, func() (int, error) {
		calls++
		return 0, errors.New("unauthorized")
	})
	if err == nil || err.Error() != "unauthorized" {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestOpenStreamWithRetry_ZeroConfigUsesDefaults(t *testing.T) {
	calls := 0
	cfg := config.RetrySettings{}
	gw := config.LLMGatewayConfig{Retry: cfg}
	gw.ApplyDefaults()
	_, err := OpenStreamWithRetry(context.Background(), gw.Retry, nil, func() (int, error) {
		calls++
		return 0, errors.New("upstream overloaded")
	})
	if err == nil {
		t.Fatal("expected exhausted error")
	}
	// MaxRetries=2 means 1 initial + 2 retries = 3 attempts.
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestShouldRetryStreamChunk_RetryableErrorBeforeAnyData(t *testing.T) {
	if !ShouldRetryStreamChunk(true, false) {
		t.Fatal("want retry before data")
	}
}

func TestShouldRetryStreamChunk_AfterDataWrittenFalse(t *testing.T) {
	if ShouldRetryStreamChunk(true, true) {
		t.Fatal("must not retry after data is written")
	}
	if ShouldRetryStreamChunk(false, false) {
		t.Fatal("must not retry non-retryable leading error")
	}
}

func TestIsStreamDeadlineExceeded(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
		want bool
	}{
		{"deadline_err", context.DeadlineExceeded, "", true},
		{"wrapped", fmt.Errorf("wrap: %w", context.DeadlineExceeded), "", true},
		{"msg", nil, "context deadline exceeded", true},
		{"client_wrap", nil, "stream read error: context deadline exceeded", true},
		{"exit1", nil, "exit status 1", false},
		{"empty", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsStreamDeadlineExceeded(tt.err, tt.msg)
			if got != tt.want {
				t.Errorf("IsStreamDeadlineExceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}
