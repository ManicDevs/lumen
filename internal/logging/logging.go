// Package logging configures structured logging (via the standard
// library's log/slog) for the application: human-readable text to stderr
// by default, or JSON when LOG_FORMAT=json — the latter is what you want
// feeding into a log aggregator in a real deployment.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New builds a slog.Logger. format is "text" or "json"; level is one of
// "debug", "info", "warn", "error" (case-insensitive, defaults to "info").
func New(w io.Writer, format string, level string) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}
