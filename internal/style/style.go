// Package style provides minimal, dependency-free ANSI styling for Lumen's
// terminal output. Styling is automatically disabled when stdout is not a
// terminal (e.g. piped to a file or another program, or run in CI) or when
// the NO_COLOR environment variable is set (https://no-color.org), so
// scripted and non-interactive usage always gets clean, uncolored text.
package style

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var isTTY = detectTTY()
var enabled = isTTY && os.Getenv("NO_COLOR") == ""

func detectTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// TTY reports whether stdout is an interactive terminal, independent of
// NO_COLOR. Useful for gating other terminal-only redraw behavior (e.g. a
// progress spinner) that would corrupt piped or logged output even when
// color itself is intentionally suppressed.
var TTY = isTTY

const (
	codeReset = "\033[0m"
	codeBold  = "\033[1m"
	codeDim   = "\033[2m"
	codeCyan  = "\033[36m"
	codeRed   = "\033[31m"
)

func wrap(code, s string) string {
	if !enabled || s == "" {
		return s
	}
	return code + s + codeReset
}

// Bold emphasizes headings and titles.
func Bold(s string) string { return wrap(codeBold, s) }

// Dim renders secondary/metadata text (dividers, labels, status footers).
func Dim(s string) string { return wrap(codeDim, s) }

// Cyan highlights interactive elements like the input prompt.
func Cyan(s string) string { return wrap(codeCyan, s) }

// Red flags errors.
func Red(s string) string { return wrap(codeRed, s) }

// Spinner is a small animated "waiting" indicator for the terminal, meant
// to cover the gap between dispatching a request and the first streamed
// token — which, for local CPU inference on a large prompt, can be many
// seconds of otherwise-silent prompt processing before generation starts.
// It is a no-op when stdout isn't a real terminal, so it never corrupts
// piped or logged output.
type Spinner struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewSpinner starts the spinner immediately, printing label after an
// animated frame. Call Stop to clear it.
func NewSpinner(label string) *Spinner {
	s := &Spinner{stop: make(chan struct{}), done: make(chan struct{})}
	if !TTY {
		close(s.done)
		return s
	}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Print("\r\033[K")
				return
			case <-ticker.C:
				fmt.Printf("\r%s %s", Dim(spinnerFrames[i%len(spinnerFrames)]), Dim(label))
				i++
			}
		}
	}()
	return s
}

// Stop halts the animation and clears the spinner line. Safe to call more
// than once and safe to call even if the spinner was never displayed.
func (s *Spinner) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}
