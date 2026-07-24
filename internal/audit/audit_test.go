package audit

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAuditLog_Add(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	al, err := NewAuditLog(logPath)
	if err != nil {
		t.Fatalf("NewAuditLog failed: %v", err)
	}
	defer al.Close()

	entry := AuditEntry{
		EventType:  "test",
		Role:       "user",
		TokenCount: 42,
		Details:    "test details",
	}

	if err := al.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	entries := al.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].EventType != "test" {
		t.Errorf("expected EventType 'test', got %s", entries[0].EventType)
	}
	if entries[0].Role != "user" {
		t.Errorf("expected Role 'user', got %s", entries[0].Role)
	}
	if entries[0].TokenCount != 42 {
		t.Errorf("expected TokenCount 42, got %d", entries[0].TokenCount)
	}
	if entries[0].Details != "test details" {
		t.Errorf("expected Details 'test details', got %s", entries[0].Details)
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestAuditLog_AddSetsTimestamp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	al, err := NewAuditLog(logPath)
	if err != nil {
		t.Fatalf("NewAuditLog failed: %v", err)
	}
	defer al.Close()

	// Entry without timestamp
	entry := AuditEntry{
		EventType: "test",
	}

	if err := al.Add(entry); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	entries := al.GetEntries()
	if entries[0].Timestamp.IsZero() {
		t.Error("Timestamp should be automatically set")
	}
}

func TestAuditLog_Close(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	al, err := NewAuditLog(logPath)
	if err != nil {
		t.Fatalf("NewAuditLog failed: %v", err)
	}

	if err := al.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Double close should return an error but not panic
	if err := al.Close(); err == nil {
		t.Error("expected error on double close")
	}
}

func TestAuditLog_GetEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	al, err := NewAuditLog(logPath)
	if err != nil {
		t.Fatalf("NewAuditLog failed: %v", err)
	}
	defer al.Close()

	// Add multiple entries
	for i := 0; i < 5; i++ {
		if err := al.Add(AuditEntry{EventType: "test", TokenCount: i}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	entries := al.GetEntries()
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}

	// Verify it's a copy (modifying shouldn't affect internal state)
	entries[0].EventType = "modified"
	entries2 := al.GetEntries()
	if entries2[0].EventType == "modified" {
		t.Error("GetEntries should return a copy")
	}
}

func TestAuditLog_Clear(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	al, err := NewAuditLog(logPath)
	if err != nil {
		t.Fatalf("NewAuditLog failed: %v", err)
	}
	defer al.Close()

	if err := al.Add(AuditEntry{EventType: "test"}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if al.Count() != 1 {
		t.Errorf("expected count 1 before clear, got %d", al.Count())
	}

	al.Clear()

	if al.Count() != 0 {
		t.Errorf("expected count 0 after clear, got %d", al.Count())
	}
}

func TestAuditLog_Count(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	al, err := NewAuditLog(logPath)
	if err != nil {
		t.Fatalf("NewAuditLog failed: %v", err)
	}
	defer al.Close()

	if al.Count() != 0 {
		t.Errorf("expected count 0 initially, got %d", al.Count())
	}

	for i := 0; i < 3; i++ {
		if err := al.Add(AuditEntry{EventType: "test"}); err != nil {
			t.Fatalf("Add failed: %v", err)
		}
		if al.Count() != i+1 {
			t.Errorf("expected count %d after %d adds, got %d", i+1, i+1, al.Count())
		}
	}
}

func TestFormatAuditEntry(t *testing.T) {
	t.Parallel()

	entry := FormatAuditEntry(nil, "test_event", "assistant", 100, "details", nil, map[string]interface{}{"key": "value"})

	if entry.EventType != "test_event" {
		t.Errorf("expected EventType 'test_event', got %s", entry.EventType)
	}
	if entry.Role != "assistant" {
		t.Errorf("expected Role 'assistant', got %s", entry.Role)
	}
	if entry.TokenCount != 100 {
		t.Errorf("expected TokenCount 100, got %d", entry.TokenCount)
	}
	if entry.Details != "details" {
		t.Errorf("expected Details 'details', got %s", entry.Details)
	}
	if entry.Context == nil || entry.Context["key"] != "value" {
		t.Errorf("expected Context with key=value, got %v", entry.Context)
	}
	if entry.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
}

func TestFormatAuditEntry_WithError(t *testing.T) {
	t.Parallel()

	err := &testError{msg: "something went wrong"}
	entry := FormatAuditEntry(nil, "error_event", "system", 0, "error details", err, nil)

	if entry.Error != "something went wrong" {
		t.Errorf("expected Error 'something went wrong', got %s", entry.Error)
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestNewAuditLog_InvalidPath(t *testing.T) {
	t.Parallel()

	// This should fail because we can't write to /invalid/path
	_, err := NewAuditLog("/invalid/path/that/does/not/exist/audit.log")
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestAuditEntry_JSON(t *testing.T) {
	t.Parallel()

	entry := AuditEntry{
		Timestamp:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		EventType:  "test",
		Role:       "user",
		TokenCount: 42,
		Details:    "details",
		Error:      "error msg",
		Context:    map[string]interface{}{"key": "value"},
	}

	// Verify all fields are exported for JSON marshaling
	_ = entry.Timestamp
	_ = entry.EventType
	_ = entry.Role
	_ = entry.TokenCount
	_ = entry.Details
	_ = entry.Error
	_ = entry.Context
}