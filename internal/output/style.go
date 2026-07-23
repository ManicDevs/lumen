package output

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var isTTY = detectTTY()
var colorEnabled = isTTY && os.Getenv("NO_COLOR") == ""

// detectTTY reports whether stdout is a character device (true terminal).
func detectTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// TTY reports whether stdout is a terminal. Used by Spinner to decide
// whether to show animated output.
var TTY = isTTY

const (
	codeReset = "\033[0m"
	codeBold  = "\033[1m"
	codeDim   = "\033[2m"
	codeCyan  = "\033[36m"
	codeRed   = "\033[31m"
)

// wrap applies an ANSI escape code around s if color is enabled.
func wrap(code, s string) string {
	if !colorEnabled || s == "" {
		return s
	}
	return code + s + codeReset
}

// Bold returns s wrapped in ANSI bold escape codes (no-op if !colorEnabled).
func Bold(s string) string { return wrap(codeBold, s) }

// Dim returns s wrapped in ANSI dim escape codes.
func Dim(s string) string { return wrap(codeDim, s) }

// Cyan returns s wrapped in ANSI cyan escape codes.
func Cyan(s string) string { return wrap(codeCyan, s) }

// Red returns s wrapped in ANSI red escape codes.
func Red(s string) string { return wrap(codeRed, s) }

type Spinner struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewSpinner starts a terminal spinner with the given label. It is a no-op
// when stdout is not a terminal.
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

// Stop terminates the spinner and restores the cursor line.
func (s *Spinner) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}
