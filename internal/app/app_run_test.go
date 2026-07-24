package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

func mockOllama(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunk, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{"role": "assistant", "content": "ok"},
			"done":    true,
		})
		w.Write(chunk)
		w.Write([]byte("\n"))
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunk, _ := json.Marshal(map[string]interface{}{"response": "ok", "done": true})
		w.Write(chunk)
		w.Write([]byte("\n"))
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"models": []map[string]interface{}{}})
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"modelfile": "FROM test"})
	})
	return httptest.NewServer(mux)
}

func TestRun_DatasetInit(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	code := Run([]string{"--dataset-init"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_Train(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	srv := mockOllama(t)
	defer srv.Close()
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	code := Run([]string{"--train"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_TrainAll(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	srv := mockOllama(t)
	defer srv.Close()
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	code := Run([]string{"--train-all"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_ConfigError_InvalidLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "INVALID_LEVEL")
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	code := Run([]string{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_InteractiveMode_Exit(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")
	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("exit\n")
		pw.Close()
	}()
	code := Run([]string{})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_InteractiveMode_Quit(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")
	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("quit\n")
		pw.Close()
	}()
	code := Run([]string{})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_InteractiveMode_EmptyLines(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")
	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("\n\nexit\n")
		pw.Close()
	}()
	code := Run([]string{})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_ChatMode_Exit(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")
	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("exit\n")
		pw.Close()
	}()
	code := Run([]string{"--chat"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_MissingTarget_NoSourceFiles(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	dir := t.TempDir()
	emptyDir := dir + "/empty"
	os.MkdirAll(emptyDir, 0755)
	t.Setenv("LUMEN_OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")
	code := Run([]string{emptyDir})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestMakeExchange_NilAuditLog(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("test")
	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return "engine", "reply", nil
	}
	exchange := makeExchange(hist, sendMsg, nil, slog.Default())
	exchange()
	snap := hist.Snapshot()
	if len(snap) < 2 {
		t.Errorf("expected at least 2 messages, got %d", len(snap))
	}
}

func TestMakeExchange_AuditLog(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("test context")
	logger := slog.Default()
	dir := t.TempDir()
	auditLog, err := session.OpenAuditLog(dir+"/audit.jsonl", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer auditLog.Close()
	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		if onToken != nil {
			onToken("test ")
			onToken("response")
		}
		return "test-engine", "test response", nil
	}
	exchange := makeExchange(hist, sendMsg, auditLog, logger)
	exchange()
	snap := hist.Snapshot()
	found := false
	for _, m := range snap {
		if m.Role == "assistant" && m.Content == "test response" {
			found = true
		}
	}
	if !found {
		t.Error("expected assistant response in history")
	}
}

func TestMakeExchange_NilOnToken(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("test context")
	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return "engine", "reply", nil
	}
	exchange := makeExchange(hist, sendMsg, nil, slog.Default())
	exchange()
	snap := hist.Snapshot()
	found := false
	for _, m := range snap {
		if m.Role == "assistant" && m.Content == "reply" {
			found = true
		}
	}
	if !found {
		t.Error("expected assistant response in history")
	}
}

func TestMakeExchange_ErrorResponse(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("test")
	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return "", "", context.DeadlineExceeded
	}
	exchange := makeExchange(hist, sendMsg, nil, slog.Default())
	exchange()
	snap := hist.Snapshot()
	for _, m := range snap {
		if m.Role == "assistant" && m.Content != "" {
			t.Error("should not record error response in history")
		}
	}
}
