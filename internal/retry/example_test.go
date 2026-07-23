package retry_test

import (
	"context"
	"errors"
	"fmt"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

func ExampleDo() {
	ctx := context.Background()
	cfg := retry.Config{
		MaxAttempts: 3,
		BaseDelay:   1, // minimal delay for testing
		MaxDelay:    5,
	}

	var attempts int
	err := retry.Do(ctx, cfg, func(ctx context.Context, attempt int) error {
		attempts++
		if attempt < 3 {
			return errors.New("transient error")
		}
		return nil
	})

	fmt.Println("attempts:", attempts)
	fmt.Println("err:", err)
	// Output:
	// attempts: 3
	// err: <nil>
}

func ExampleDo_permanentError() {
	ctx := context.Background()
	cfg := retry.Config{
		MaxAttempts: 5,
		BaseDelay:   1,
		MaxDelay:    5,
	}

	var attempts int
	err := retry.Do(ctx, cfg, func(ctx context.Context, attempt int) error {
		attempts++
		return retry.Permanent(errors.New("don't retry this"))
	})

	fmt.Println("attempts:", attempts)
	fmt.Println("err:", err.Error())
	// Output:
	// attempts: 1
	// err: don't retry this
}

func ExamplePermanent() {
	err := retry.Permanent(errors.New("bad config"))
	var p *retry.PermanentError
	if errors.As(err, &p) {
		fmt.Println("wrapped as permanent")
	}
	// Output: wrapped as permanent
}

func ExampleDefaultConfig() {
	fmt.Println(retry.DefaultConfig.MaxAttempts >= 1)
	// Output: true
}
