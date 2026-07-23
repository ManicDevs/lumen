// Package session provides thread-safe conversation history storage and an
// append-only JSON-lines audit log that records every user and assistant
// exchange with metadata (token counts, duration, engine name, errors).
package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// AuditEntry records a single exchange in the audit log.
type AuditEntry struct {
	Timestamp  string `json:"timestamp"`
	Role       string `json:"role"`
	TokenCount int    `json:"token_count_approx"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Engine     string `json:"engine,omitempty"`
	Error      string `json:"error,omitempty"`
}

// AuditLog is a thread-safe, append-only JSON-lines audit trail.
type AuditLog struct {
	mu     sync.Mutex
	f      *os.File
	logger *slog.Logger
}

// OpenAuditLog opens (or creates) an append-only JSON-lines audit log at the
// given path. Missing parent directories are NOT created automatically.
func OpenAuditLog(path string, logger *slog.Logger) (*AuditLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: opening audit log %s: %w", path, err)
	}
	return &AuditLog{f: f, logger: logger}, nil
}

// Write appends a JSON line to the audit log. The timestamp defaults to
// time.Now().UTC(). Marshal or write errors are logged via the AuditLog's
// logger but never returned to the caller.
func (a *AuditLog) Write(entry AuditEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	data, err := json.Marshal(entry)
	if err != nil {
		if a.logger != nil {
			a.logger.Error("audit log marshal failure", "err", err)
		}
		return
	}
	if _, err := fmt.Fprintf(a.f, "%s\n", data); err != nil {
		if a.logger != nil {
			a.logger.Error("audit log write failure", "err", err)
		}
	}
}

// Close flushes and closes the underlying audit log file.
func (a *AuditLog) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.f.Close()
}

// ApproxTokens returns a rough token count for a string (len/4). This is a
// coarse estimate used only for audit metadata, not for model context limits.
func ApproxTokens(s string) int {
	return len(s) / 4
}
