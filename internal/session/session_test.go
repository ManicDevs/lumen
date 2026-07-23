package session

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

func TestNewHistory_SeededWithInitialContext(t *testing.T) {
	t.Parallel()
	h := NewHistory("test context")
	snap := h.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 initial messages, got %d", len(snap))
	}
	if snap[0].Role != "user" || snap[0].Content != "test context" {
		t.Errorf("first message: got role=%q content=%q", snap[0].Role, snap[0].Content)
	}
	if snap[1].Role != "model" || snap[1].Content != "Indexed." {
		t.Errorf("second message: got role=%q content=%q", snap[1].Role, snap[1].Content)
	}
}

func TestHistory_Append(t *testing.T) {
	t.Parallel()
	h := NewHistory("ctx")
	h.Append(llm.ChatMessage{Role: "user", Content: "hello"})
	h.Append(llm.ChatMessage{Role: "assistant", Content: "world"})
	snap := h.Snapshot()
	if len(snap) != 4 {
		t.Fatalf("expected 4 messages after 2 appends, got %d", len(snap))
	}
	if snap[2].Content != "hello" || snap[3].Content != "world" {
		t.Errorf("unexpected appended content: %+v", snap[2:])
	}
}

func TestHistory_SnapshotIsCopy(t *testing.T) {
	t.Parallel()
	h := NewHistory("ctx")
	snap := h.Snapshot()
	snap[0].Content = "mutated"
	orig := h.Snapshot()
	if orig[0].Content == "mutated" {
		t.Error("Snapshot should return a copy, not alias the internal slice")
	}
}

func TestHistory_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	h := NewHistory("ctx")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Append(llm.ChatMessage{Role: "user", Content: "msg"})
			_ = h.Snapshot()
		}()
	}
	wg.Wait()
	snap := h.Snapshot()
	if len(snap) != 22 {
		t.Errorf("expected 22 total messages after 20 concurrent appends, got %d", len(snap))
	}
}

func TestHistory_Render(t *testing.T) {
	t.Parallel()
	h := NewHistory("initial context")
	h.Append(llm.ChatMessage{Role: "user", Content: "hello"})
	h.Append(llm.ChatMessage{Role: "assistant", Content: "world"})
	rendered := h.Render()
	if !strings.Contains(rendered, "### USER") {
		t.Errorf("expected ### USER in render, got: %q", rendered)
	}
	if !strings.Contains(rendered, "### ASSISTANT") {
		t.Errorf("expected ### ASSISTANT in render, got: %q", rendered)
	}
	if !strings.Contains(rendered, "hello") {
		t.Errorf("expected 'hello' in render, got: %q", rendered)
	}
	if !strings.Contains(rendered, "world") {
		t.Errorf("expected 'world' in render, got: %q", rendered)
	}
}

func TestApproxTokens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{"hello world, this is a test", 6},
	}
	for _, tc := range cases {
		got := ApproxTokens(tc.input)
		if got != tc.want {
			t.Errorf("ApproxTokens(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestOpenAuditLog_CreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	al, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatalf("OpenAuditLog failed: %v", err)
	}
	defer al.Close()

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected audit log file to exist: %v", err)
	}
}

func TestOpenAuditLog_AppendsEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	al, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatalf("OpenAuditLog failed: %v", err)
	}

	al.Write(AuditEntry{Role: "user", TokenCount: 5})
	al.Write(AuditEntry{Role: "assistant", TokenCount: 20, Engine: "Ollama"})
	al.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], `"role":"user"`) {
		t.Errorf("expected user entry, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"role":"assistant"`) && !strings.Contains(lines[1], `"engine":"Ollama"`) {
		t.Errorf("expected assistant entry, got: %s", lines[1])
	}
}

func TestAuditLog_WritesTimestamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	al, err := OpenAuditLog(path, nil)
	if err != nil {
		t.Fatalf("OpenAuditLog failed: %v", err)
	}
	al.Write(AuditEntry{Role: "user", TokenCount: 1})
	al.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	if !strings.Contains(string(data), "timestamp") {
		t.Errorf("expected timestamp in audit entry: %s", data)
	}
}
