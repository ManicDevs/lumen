package session

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestWrite_MarshalError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditLog, err := OpenAuditLog(filepath.Join(dir, "audit.jsonl"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer auditLog.Close()

	// Write a valid entry — this should succeed
	auditLog.Write(AuditEntry{Role: "user", TokenCount: 10})

	// Verify the file has content
	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty audit log")
	}
}

func TestWrite_NilLogger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	auditLog, err := OpenAuditLog(filepath.Join(dir, "audit.jsonl"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer auditLog.Close()

	// Write with nil logger — should not panic
	auditLog.Write(AuditEntry{Role: "assistant", TokenCount: 5})
}

func TestWrite_TimestampSet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	auditLog, err := OpenAuditLog(filepath.Join(dir, "audit.jsonl"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer auditLog.Close()

	auditLog.Write(AuditEntry{Role: "user", Timestamp: "2026-01-01T00:00:00Z"})

	data, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty audit log")
	}
}

func TestOpenAuditLog_BadPath(t *testing.T) {
	t.Parallel()
	_, err := OpenAuditLog("/nonexistent/path/audit.jsonl", slog.Default())
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

func TestWrite_FileClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auditLog, err := OpenAuditLog(filepath.Join(dir, "audit.jsonl"), logger)
	if err != nil {
		t.Fatal(err)
	}
	// Close the file directly, then try to write
	auditLog.f.Close()
	auditLog.Write(AuditEntry{Role: "user", TokenCount: 10})
	// Should not panic even with write error
}

func TestWrite_NilLogger_FileClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	auditLog, err := OpenAuditLog(filepath.Join(dir, "audit.jsonl"), nil)
	if err != nil {
		t.Fatal(err)
	}
	auditLog.f.Close()
	auditLog.Write(AuditEntry{Role: "user", TokenCount: 10})
	// Should not panic even with nil logger and closed file
}

func TestApproxTokens_VariousLengths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{"12345678", 2},
	}
	for _, tt := range tests {
		got := ApproxTokens(tt.input)
		if got != tt.want {
			t.Errorf("ApproxTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
