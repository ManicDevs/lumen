package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPermanent_NilError(t *testing.T) {
	t.Parallel()
	got := Permanent(nil)
	if got != nil {
		t.Errorf("Permanent(nil) = %v, want nil", got)
	}
}

func TestPermanent_WrapsError(t *testing.T) {
	t.Parallel()
	orig := errors.New("original")
	p := Permanent(orig)
	var pe *PermanentError
	if !errors.As(p, &pe) {
		t.Fatal("expected PermanentError")
	}
	if pe.Err != orig {
		t.Errorf("wrapped error = %v, want %v", pe.Err, orig)
	}
	if pe.Error() != "original" {
		t.Errorf("Error() = %q, want %q", pe.Error(), "original")
	}
	if pe.Unwrap() != orig {
		t.Errorf("Unwrap() = %v, want %v", pe.Unwrap(), orig)
	}
}

func TestDo_MaxAttemptsZero(t *testing.T) {
	t.Parallel()
	calls := 0
	err := Do(context.Background(), Config{MaxAttempts: 0, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(ctx context.Context, attempt int) error {
		calls++
		if attempt == 1 {
			return nil
		}
		return errors.New("should not reach")
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestDo_MaxAttemptsExceeds100(t *testing.T) {
	t.Parallel()
	err := Do(context.Background(), Config{MaxAttempts: 101, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(ctx context.Context, attempt int) error {
		return errors.New("should not be called")
	})
	if err == nil {
		t.Fatal("expected error for MaxAttempts > 100")
	}
}

func TestDo_BaseDelayZero(t *testing.T) {
	t.Parallel()
	calls := 0
	err := Do(context.Background(), Config{MaxAttempts: 2, BaseDelay: 0, MaxDelay: time.Second}, func(ctx context.Context, attempt int) error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestDo_MaxDelayZero(t *testing.T) {
	t.Parallel()
	calls := 0
	err := Do(context.Background(), Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 0}, func(ctx context.Context, attempt int) error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestDo_ContextCancelledBetweenRetries(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Do(ctx, Config{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(ctx context.Context, attempt int) error {
		calls++
		if calls == 1 {
			cancel()
		}
		return errors.New("fail")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
