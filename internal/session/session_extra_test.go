package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

func TestNewHistory_EmptyContext(t *testing.T) {
	t.Parallel()
	h := NewHistory("")
	snap := h.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(snap))
	}
	if snap[0].Content != "" {
		t.Errorf("expected empty initial context, got %q", snap[0].Content)
	}
	if snap[1].Role != "model" || snap[1].Content != "Indexed." {
		t.Errorf("expected model/Indexed., got %q/%q", snap[1].Role, snap[1].Content)
	}
}

func TestHistory_RenderModelRoleMapsToAssistant(t *testing.T) {
	t.Parallel()
	h := NewHistory("ctx")
	h.Append(llm.ChatMessage{Role: "model", Content: "hello from model"})
	rendered := h.Render()
	if !strings.Contains(rendered, "### ASSISTANT") {
		t.Error("expected '### ASSISTANT' in rendered output for model role")
	}
	if !strings.Contains(rendered, "hello from model") {
		t.Error("expected message content in rendered output")
	}
}

func TestHistory_RenderAllRoles(t *testing.T) {
	t.Parallel()
	h := NewHistory("ctx")
	h.Append(llm.ChatMessage{Role: "user", Content: "question"})
	h.Append(llm.ChatMessage{Role: "assistant", Content: "answer"})
	rendered := h.Render()
	if !strings.Contains(rendered, "### USER") {
		t.Error("expected ### USER")
	}
	if !strings.Contains(rendered, "### ASSISTANT") {
		t.Error("expected ### ASSISTANT")
	}
}

func TestAuditLog_CloseIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	log, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// Second close returns an error (file already closed) — that's expected behavior
	err = log.Close()
	if err == nil {
		t.Log("second Close returned nil — acceptable")
	}
}

func TestAuditLog_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	log, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Write(AuditEntry{Role: "user", TokenCount: 10})
		}()
	}
	wg.Wait()
	log.Close()

	// Count lines in file
	lines := countLines(t, path)
	if lines != 50 {
		t.Errorf("expected 50 lines, got %d", lines)
	}
}

func TestAuditLog_WritesAllFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	log, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	log.Write(AuditEntry{
		Role:       "assistant",
		TokenCount: 42,
		DurationMS: 1234,
		Engine:     "test-engine",
		Error:      "some error",
		Timestamp:  "2025-01-01T00:00:00Z",
	})
	log.Close()

	entries := readAuditEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.TokenCount != 42 {
		t.Errorf("TokenCount = %d", e.TokenCount)
	}
	if e.DurationMS != 1234 {
		t.Errorf("DurationMS = %d", e.DurationMS)
	}
	if e.Engine != "test-engine" {
		t.Errorf("Engine = %q", e.Engine)
	}
	if e.Error != "some error" {
		t.Errorf("Error = %q", e.Error)
	}
	if e.Timestamp != "2025-01-01T00:00:00Z" {
		t.Errorf("Timestamp = %q", e.Timestamp)
	}
}

func TestOpenAuditLog_InvalidPath(t *testing.T) {
	t.Parallel()
	_, err := OpenAuditLog("/nonexistent/dir/audit.jsonl", nil)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestAuditLog_WriteWithAutoTimestamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	log, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	log.Write(AuditEntry{Role: "user", TokenCount: 5})
	log.Close()

	entries := readAuditEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Timestamp == "" {
		t.Error("expected auto-generated timestamp")
	}
}

func TestHistory_ConcurrentAppendSnapshotRender(t *testing.T) {
	t.Parallel()
	h := NewHistory("ctx")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			h.Append(llm.ChatMessage{Role: "user", Content: "x"})
		}()
		go func() {
			defer wg.Done()
			_ = h.Snapshot()
		}()
		go func() {
			defer wg.Done()
			_ = h.Render()
		}()
	}
	wg.Wait()
	snap := h.Snapshot()
	if len(snap) != 22 {
		t.Errorf("expected 22 messages, got %d", len(snap))
	}
}

func TestApproxTokens_Various(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{"hello world", 2},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := ApproxTokens(tc.input)
			if got != tc.want {
				t.Errorf("ApproxTokens(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestAuditLog_WriteWithLogger_MarshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	log, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	// Write valid entry
	log.Write(AuditEntry{Role: "user", TokenCount: 1})
	log.Close()

	entries := readAuditEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

func readAuditEntries(t *testing.T, path string) []AuditEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var entries []AuditEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("unmarshal: %v", err)
			continue
		}
		entries = append(entries, e)
	}
	return entries
}
