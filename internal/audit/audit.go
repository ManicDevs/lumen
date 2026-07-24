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

type AuditLog struct {
	entries   []AuditEntry
	mu        sync.RWMutex
	logWriter *os.File
	filePath  string
}

type AuditEntry struct {
	Timestamp  time.Time              `json:"timestamp"`
	EventType  string                 `json:"event_type"`
	Role       string                 `json:"role"`
	TokenCount int                    `json:"token_count"`
	Details    string                 `json:"details,omitempty"`
	Error      string                 `json:"error,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
}

type AuditLogConfig struct {
	SyncOnWrite      bool  `json:"sync_on_write"`
	MaxFileSize      int64 `json:"max_file_size"`
	CompressionLevel int   `json:"compression_level"`
}

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

func (al *AuditLog) Close() error {
	if al.logWriter != nil {
		if err := al.logWriter.Close(); err != nil {
			return fmt.Errorf("failed to close audit log: %w", err)
		}
	}
	return nil
}

func (al *AuditLog) GetEntries() []AuditEntry {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return append([]AuditEntry{}, al.entries...)
}

func (al *AuditLog) Clear() {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.entries = []AuditEntry{}
}

func (al *AuditLog) Count() int {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return len(al.entries)
}

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
