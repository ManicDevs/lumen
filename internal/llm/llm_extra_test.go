package llm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

func TestOpenAI_Send_500_Retriable_Then_Success2(t *testing.T) {
	t.Parallel()
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"server error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n"))
		w.Write([]byte("data: [DONE]\n"))
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if reply != "recovered" {
		t.Errorf("reply = %q, want %q", reply, "recovered")
	}
}

func TestOpenAI_Send_NetworkError2(t *testing.T) {
	t.Parallel()
	eng := NewOpenAIEngine("http://localhost:1", "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestOpenAI_Send_EmptyResponse_Retries2(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: [DONE]\n"))
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestOllama_Send_NilLogger2(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message":{"content":"ok"},"done":true}`))
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1}, nil)
	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q, want %q", reply, "ok")
	}
}

func TestLocalEngine_Defaults2(t *testing.T) {
	t.Parallel()
	eng := NewLocalEngine("http://localhost:11434", "model", "sys", 0, 0, retry.Config{}, slog.Default())
	if eng.Name() != "Ollama" {
		t.Errorf("Name() = %q, want %q", eng.Name(), "Ollama")
	}
}

func TestOpenAIEngine_Defaults2(t *testing.T) {
	t.Parallel()
	eng := NewOpenAIEngine("http://localhost:1234/", "model", "sys", 0, retry.Config{}, slog.Default())
	if eng.Name() != "OpenAI-compat" {
		t.Errorf("Name() = %q, want %q", eng.Name(), "OpenAI-compat")
	}
}

func TestExtractErrorReason_LongText2(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	got := extractErrorReason(long)
	if len(got) > 210 {
		t.Errorf("expected truncated, got len=%d", len(got))
	}
}

func TestExtractErrorReason_AltShape2(t *testing.T) {
	got := extractErrorReason([]byte(`{"error":"something went wrong"}`))
	if got != "something went wrong" {
		t.Errorf("expected 'something went wrong', got %q", got)
	}
}

func TestApiErrorMessage2(t *testing.T) {
	got := apiErrorMessage("Ollama", 500, []byte(`{"error":{"message":"server error"}}`))
	if got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestApiErrorMessage_NoBody2(t *testing.T) {
	got := apiErrorMessage("Backend", 404, []byte{})
	if got == "" {
		t.Error("expected non-empty error message")
	}
}
