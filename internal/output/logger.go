// Package output provides terminal styling, structured logging, secret
// redaction, and a spinner widget for CLI progress indication.
package output

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a slog.Logger that writes to w. Format can be "text"
// (default) or "json". Level can be "debug", "info", "warn", or "error".
func NewLogger(w io.Writer, format string, level string) *slog.Logger {
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
