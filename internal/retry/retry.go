// Package retry implements a small exponential-backoff retry helper for
// transient failures (network errors, 5xx, 429), while treating permanent
// failures (4xx like 404/401/403) as non-retryable so we fail fast instead
// of hammering a broken configuration.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// PermanentError wraps an error to signal it should NOT be retried —
// e.g. a 404 (wrong model name) or 401/403 (bad credentials). Retrying
// those just wastes time and quota.
type PermanentError struct {
	Err error
}

func (p *PermanentError) Error() string { return p.Err.Error() }
func (p *PermanentError) Unwrap() error { return p.Err }

// Permanent marks err as non-retryable.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

func isPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// Config controls retry behavior.
type Config struct {
	MaxAttempts int           // total attempts including the first, e.g. 4 = 1 try + 3 retries
	BaseDelay   time.Duration // delay before the first retry
	MaxDelay    time.Duration // cap on backoff growth
}

// DefaultConfig is a sane default for network calls to an external API.
var DefaultConfig = Config{
	MaxAttempts: 4,
	BaseDelay:   500 * time.Millisecond,
	MaxDelay:    8 * time.Second,
}

// Do runs fn, retrying on transient failure with exponential backoff plus
// jitter, up to cfg.MaxAttempts. It stops immediately (no retry) if fn
// returns a PermanentError, or if ctx is cancelled. The last error
// encountered is returned if all attempts are exhausted.
func Do(ctx context.Context, cfg Config, fn func(ctx context.Context, attempt int) error) error {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}

	var lastErr error
	delay := cfg.BaseDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn(ctx, attempt)
		if err == nil {
			return nil
		}
		lastErr = err

		if isPermanent(err) {
			return err
		}
		if attempt == cfg.MaxAttempts {
			break
		}

		// Exponential backoff with +/-20% jitter, capped at MaxDelay.
		jitter := time.Duration(float64(delay) * (0.8 + 0.4*rand.Float64()))
		wait := jitter
		if wait > cfg.MaxDelay {
			wait = cfg.MaxDelay
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		delay *= 2
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	return lastErr
}
