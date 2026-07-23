// Package session manages per-run state: the structured audit log (JSONL)
// and the in-memory conversation history passed to each engine call.
package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/engine"
)

// AuditEntry is one line in the JSONL audit log — one entry per exchange.
type AuditEntry struct {
	Timestamp  string `json:"timestamp"`
	Role       string `json:"role"`
	TokenCount int    `json:"token_count_approx"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Engine     string `json:"engine,omitempty"`
	Error      string `json:"error,omitempty"`
}

// AuditLog writes structured JSONL entries to a file and to the provided
// slog.Logger. It is safe for concurrent use.
type AuditLog struct {
	mu     sync.Mutex
	f      *os.File
	logger *slog.Logger
}

// OpenAuditLog opens (or creates) the JSONL audit log at path. Caller
// must call Close() when done.
func OpenAuditLog(path string, logger *slog.Logger) (*AuditLog, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: opening audit log %s: %w", path, err)
	}
	return &AuditLog{f: f, logger: logger}, nil
}

// Write appends a single entry to the audit log.
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

// Close flushes and closes the underlying file.
func (a *AuditLog) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.f.Close()
}

// ApproxTokens is a rough estimate (chars/4) — just for the audit log.
func ApproxTokens(s string) int {
	return len(s) / 4
}

// History is the in-memory conversation history for one session.
type History struct {
	mu       sync.RWMutex
	messages []engine.ChatMessage
}

// New returns a History seeded with the initial context exchange.
func New(initialContext string) *History {
	return &History{
		messages: []engine.ChatMessage{
			{Role: "user", Content: initialContext},
			{Role: "model", Content: "Indexed."},
		},
	}
}

// Append adds a message to the history. Safe for concurrent use.
func (h *History) Append(msg engine.ChatMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
}

// Snapshot returns a copy of the current history slice for passing to an
// engine call, so the caller can't alias the internal slice.
func (h *History) Snapshot() []engine.ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]engine.ChatMessage, len(h.messages))
	copy(out, h.messages)
	return out
}

// Render formats the full history as a plain-text/Markdown transcript
// suitable for writing to a file via /save. Roles are normalized ("model"
// -> "assistant") for readability.
func (h *History) Render() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var b strings.Builder
	for _, m := range h.messages {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", strings.ToUpper(role), m.Content))
	}
	return b.String()
}
