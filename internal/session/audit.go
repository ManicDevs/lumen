package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

type AuditEntry struct {
	Timestamp  string `json:"timestamp"`
	Role       string `json:"role"`
	TokenCount int    `json:"token_count_approx"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Engine     string `json:"engine,omitempty"`
	Error      string `json:"error,omitempty"`
}

type AuditLog struct {
	mu     sync.Mutex
	f      *os.File
	logger *slog.Logger
}

func OpenAuditLog(path string, logger *slog.Logger) (*AuditLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: opening audit log %s: %w", path, err)
	}
	return &AuditLog{f: f, logger: logger}, nil
}

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

func (a *AuditLog) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.f.Close()
}

func ApproxTokens(s string) int {
	return len(s) / 4
}
