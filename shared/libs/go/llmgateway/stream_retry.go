package llmgateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

// RetryBudget tracks bounded stream open and leading-chunk retries.
type RetryBudget struct {
	cfg   config.RetrySettings
	fails int
}

// NewRetryBudget applies safe defaults for an all-zero RetrySettings.
func NewRetryBudget(cfg config.RetrySettings) *RetryBudget {
	return &RetryBudget{cfg: normalizeRetrySettings(cfg)}
}

func normalizeRetrySettings(cfg config.RetrySettings) config.RetrySettings {
	unset := cfg.MaxRetries == 0 && cfg.InitialDelaySeconds == 0 && cfg.MaxDelaySeconds == 0
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	if unset {
		cfg.InitialDelaySeconds = 1
		cfg.MaxDelaySeconds = 8
	} else if cfg.MaxDelaySeconds == 0 {
		cfg.MaxDelaySeconds = 8
	}
	return cfg
}

func retryBackoff(cfg config.RetrySettings, attempt int) time.Duration {
	if cfg.InitialDelaySeconds <= 0 {
		return 0
	}
	d := time.Duration(cfg.InitialDelaySeconds) * time.Second
	for i := 0; i < attempt; i++ {
		d *= 2
	}
	max := time.Duration(cfg.MaxDelaySeconds) * time.Second
	if max <= 0 {
		max = 8 * time.Second
	}
	if d > max {
		d = max
	}
	return d
}

// IsRetryableStreamErr reports whether an open/chunk error is a transient upstream failure.
func IsRetryableStreamErr(err error, msg string) bool {
	if err != nil {
		if codingagent.IsRetryableError(err) || codingagent.IsRetryableUpstream(err.Error()) {
			return true
		}
	}
	return codingagent.IsRetryableUpstream(msg)
}

const LogUpstreamStreamDeadline = "upstream stream read deadline exceeded"

// IsStreamDeadlineExceeded reports a client or upstream read that hit a context deadline.
func IsStreamDeadlineExceeded(err error, msg string) bool {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return true
		}
	}
	return strings.Contains(msg, "context deadline exceeded")
}

// LogIfStreamDeadline writes the unique operator Error when the stream failed on a deadline.
func LogIfStreamDeadline(log logger.Logger, err error, msg, model string) {
	if log == nil || !IsStreamDeadlineExceeded(err, msg) {
		return
	}
	if msg == "" && err != nil {
		msg = err.Error()
	}
	log.Debug("classified stream error as upstream deadline", "model", model)
	log.Error(LogUpstreamStreamDeadline, "model", model, "error", msg)
}

// ShouldRetryStreamChunk is true only for retryable errors before any success data.
func ShouldRetryStreamChunk(retryable, dataWritten bool) bool {
	return retryable && !dataWritten
}

// OpenStreamWithRetry opens a stream up to 1+MaxRetries times on retryable errors.
func OpenStreamWithRetry[T any](
	ctx context.Context,
	cfg config.RetrySettings,
	log logger.Logger,
	open func() (T, error),
) (T, error) {
	return OpenWithBudget(NewRetryBudget(cfg), ctx, log, open)
}

// OpenWithBudget opens a stream using a shared retry budget (start + leading chunks).
func OpenWithBudget[T any](
	b *RetryBudget,
	ctx context.Context,
	log logger.Logger,
	open func() (T, error),
) (T, error) {
	var zero T
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		out, err := open()
		if err == nil {
			return out, nil
		}
		if !IsRetryableStreamErr(err, err.Error()) || b.fails >= b.cfg.MaxRetries {
			return zero, err
		}
		delay := retryBackoff(b.cfg, b.fails)
		if log != nil {
			log.Warn("retrying upstream stream open",
				"attempt", b.fails+1,
				"max_retries", b.cfg.MaxRetries,
				"delay", delay.String(),
				"error", err.Error())
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return zero, ctx.Err()
			}
		}
		b.fails++
	}
}

// DoWithRetry retries a non-stream request on retryable start errors.
func DoWithRetry(ctx context.Context, cfg config.RetrySettings, log logger.Logger, fn func() error) error {
	_, err := OpenStreamWithRetry(ctx, cfg, log, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// RetryLeadingChunk waits and consumes one retry when a leading error chunk is retryable.
func (b *RetryBudget) RetryLeadingChunk(ctx context.Context, log logger.Logger, msg string, dataWritten bool) bool {
	retryable := codingagent.IsRetryableUpstream(msg)
	if !ShouldRetryStreamChunk(retryable, dataWritten) {
		return false
	}
	if b.fails >= b.cfg.MaxRetries {
		return false
	}
	delay := retryBackoff(b.cfg, b.fails)
	if log != nil {
		log.Warn("retrying upstream stream after leading error chunk",
			"attempt", b.fails+1,
			"max_retries", b.cfg.MaxRetries,
			"delay", delay.String(),
			"error", msg)
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false
		}
	}
	b.fails++
	return true
}

// DiscardStream drains a leftover stream so the producer can exit.
func DiscardStream[T any](ch <-chan T) {
	if ch == nil {
		return
	}
	go func() {
		for range ch {
		}
	}()
}

// StreamErr builds an error from an upstream message.
func StreamErr(msg string) error {
	if msg == "" {
		return errors.New("upstream stream request failed")
	}
	return errors.New(msg)
}
