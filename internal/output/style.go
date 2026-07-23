package output

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var isTTY = detectTTY()
var colorEnabled = isTTY && os.Getenv("NO_COLOR") == ""

func detectTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

var TTY = isTTY

const (
	codeReset = "\033[0m"
	codeBold  = "\033[1m"
	codeDim   = "\033[2m"
	codeCyan  = "\033[36m"
	codeRed   = "\033[31m"
)

func wrap(code, s string) string {
	if !colorEnabled || s == "" {
		return s
	}
	return code + s + codeReset
}

func Bold(s string) string { return wrap(codeBold, s) }
func Dim(s string) string  { return wrap(codeDim, s) }
func Cyan(s string) string { return wrap(codeCyan, s) }
func Red(s string) string  { return wrap(codeRed, s) }

type Spinner struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

func (s *Spinner) Stop() {
	s.once.Do(func() { close(s.stop) })
	<-s.done
}
