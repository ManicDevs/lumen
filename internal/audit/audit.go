// Package audit provides a thread-safe audit log for recording structured
// events during Lumen sessions, with JSONL file persistence.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLog is a thread-safe structured audit log that persists entries to a
// JSONL file on disk. It maintains an in-memory buffer of all entries for
// fast retrieval.
type AuditLog struct {
	entries   []AuditEntry
	mu        sync.RWMutex
	logWriter *os.File
	filePath  string
}

// AuditEntry represents a single audit event, such as an LLM exchange or
// user action, with optional context metadata.
type AuditEntry struct {
	Timestamp  time.Time              `json:"timestamp"`
	EventType  string                 `json:"event_type"`
	Role       string                 `json:"role"`
	TokenCount int                    `json:"token_count"`
	Details    string                 `json:"details,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
}

// AuditLogConfig holds configuration options for an AuditLog, controlling
// sync behaviour, file size limits, and compression.
type AuditLogConfig struct {
	SyncOnWrite      bool  `json:"sync_on_write"`
	MaxFileSize      int64 `json:"max_file_size"`
	CompressionLevel int   `json:"compression_level"`
}

// NewAuditLog creates a new AuditLog that persists entries to the given
// configPath. If configPath is empty, a default path is used. The parent
// directory is created if it does not exist.
func NewAuditLog(configPath string) (*AuditLog, error) {
	if configPath == "" {
		configPath = "/var/log/lumen/audit.jsonl"
	}

	if _, err := os.Stat(filepath.Dir(configPath)); err != nil {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	log, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}

	return &AuditLog{
		logWriter: log,
		filePath:  configPath,
	}, nil
}

// Add appends an AuditEntry to the log. If the entry's Timestamp is zero,
// it is set to the current time. The entry is also persisted to the JSONL
// file when a log writer is configured.
func (al *AuditLog) Add(entry AuditEntry) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	al.entries = append(al.entries, entry)

	if al.logWriter != nil {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal audit entry: %w", err)
		}
		if _, err := al.logWriter.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("failed to write to audit log: %w", err)
		}
		al.logWriter.Sync()
	}

	return nil
}

// Close closes the underlying log file. It is safe to call multiple times.
func (al *AuditLog) Close() error {
	if al.logWriter != nil {
		if err := al.logWriter.Close(); err != nil {
			return fmt.Errorf("failed to close audit log: %w", err)
		}
	}
	return nil
}

// GetEntries returns a copy of all audit entries recorded so far.
func (al *AuditLog) GetEntries() []AuditEntry {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return append([]AuditEntry{}, al.entries...)
}

// Clear removes all in-memory entries without affecting the log file.
func (al *AuditLog) Clear() {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.entries = []AuditEntry{}
}

// Count returns the number of audit entries currently held in memory.
func (al *AuditLog) Count() int {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return len(al.entries)
}

// FormatAuditEntry constructs an AuditEntry with the given fields, setting
// the Timestamp to the current time. If err is non-nil, its error message
// is captured in the Error field.
func FormatAuditEntry(ctx context.Context, eventType, role string, tokenCount int, details string, err error, extra map[string]interface{}) AuditEntry {
	entry := AuditEntry{
		Timestamp:  time.Now(),
		EventType:  eventType,
		Role:       role,
		TokenCount: tokenCount,
		Details:    details,
		Context:    extra,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	return entry
}
