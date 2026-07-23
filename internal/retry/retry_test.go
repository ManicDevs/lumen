package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fastCfg() Config {
	return Config{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

func TestDo_SucceedsFirstTry(t *testing.T) {
	t.Parallel()
	calls := 0
	err := Do(context.Background(), fastCfg(), func(ctx context.Context, attempt int) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_RetriesTransientThenSucceeds(t *testing.T) {
	t.Parallel()
	calls := 0
	err := Do(context.Background(), fastCfg(), func(ctx context.Context, attempt int) error {
		calls++
		if calls < 3 {
			return errors.New("transient failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDo_PermanentErrorStopsImmediately(t *testing.T) {
	t.Parallel()
	calls := 0
	sentinel := errors.New("404 not found")
	err := Do(context.Background(), fastCfg(), func(ctx context.Context, attempt int) error {
		calls++
		return Permanent(sentinel)
	})
	if calls != 1 {
		t.Errorf("expected exactly 1 call for permanent error, got %d", calls)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error, got %v", err)
	}
}

func TestDo_ExhaustsAttemptsAndReturnsLastError(t *testing.T) {
	t.Parallel()
	calls := 0
	err := Do(context.Background(), fastCfg(), func(ctx context.Context, attempt int) error {
		calls++
		return errors.New("always fails")
	})
	if calls != 3 {
		t.Errorf("expected 3 attempts (MaxAttempts), got %d", calls)
	}
	if err == nil {
		t.Error("expected error after exhausting attempts")
	}
}

func TestDo_ContextCancelledStopsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(2 * time.Millisecond)
		cancel()
	}()
	err := Do(ctx, Config{MaxAttempts: 100, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}, func(ctx context.Context, attempt int) error {
		calls++
		return errors.New("always fails")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if calls >= 100 {
		t.Errorf("expected cancellation to stop retries early, got %d calls", calls)
	}
}
