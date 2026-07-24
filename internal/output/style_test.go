package output

import (
	"sync"
	"testing"
	"time"
)

func TestSpinner_NonTTYNoops(t *testing.T) {
	origTTY := TTY
	TTY = false
	t.Cleanup(func() { TTY = origTTY })

	s := NewSpinner("loading")
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop() blocked on non-TTY spinner")
	}
}

func TestSpinner_StopIdempotent(t *testing.T) {
	origTTY := TTY
	TTY = true
	t.Cleanup(func() { TTY = origTTY })

	s := NewSpinner("working")
	s.Stop()
	s.Stop()
}

func TestSpinner_ConcurrentStartStop(t *testing.T) {
	origTTY := TTY
	TTY = true
	t.Cleanup(func() { TTY = origTTY })

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewSpinner("test")
			time.Sleep(10 * time.Millisecond)
			s.Stop()
		}()
	}
	wg.Wait()
}

func TestBold_ColorEnabled(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = true
	t.Cleanup(func() { colorEnabled = origColor })

	result := Bold("hello")
	if result != "\033[1mhello\033[0m" {
		t.Errorf("expected bold ANSI codes, got %q", result)
	}
}

func TestBold_ColorDisabled(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = origColor })

	result := Bold("hello")
	if result != "hello" {
		t.Errorf("expected no formatting when color disabled, got %q", result)
	}

	result = Bold("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestDim_Cyan_Red(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = true
	t.Cleanup(func() { colorEnabled = origColor })

	if got := Dim("dimmed"); got != "\033[2mdimmed\033[0m" {
		t.Errorf("Dim: got %q", got)
	}
	if got := Cyan("cyan"); got != "\033[36mcyan\033[0m" {
		t.Errorf("Cyan: got %q", got)
	}
	if got := Red("red"); got != "\033[31mred\033[0m" {
		t.Errorf("Red: got %q", got)
	}
}

func TestWrap_EmptyString(t *testing.T) {
	origColor := colorEnabled
	colorEnabled = true
	t.Cleanup(func() { colorEnabled = origColor })

	if got := Bold(""); got != "" {
		t.Errorf("expected empty for empty input, got %q", got)
	}
}

func TestSpinner_TextEmpty(t *testing.T) {
	origTTY := TTY
	TTY = false
	t.Cleanup(func() { TTY = origTTY })

	s := NewSpinner("")
	s.Stop()
}
