package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

func TestSendOllama_SlowButSteadyStream_DoesNotTimeOut(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunks := []string{"Hello", " world", " this", " is", " slow"}
		for _, c := range chunks {
			fmt.Fprintf(w, `{"message":{"content":%q},"done":false}`+"\n", c)
			flusher.Flush()
			time.Sleep(80 * time.Millisecond)
		}
		fmt.Fprint(w, `{"message":{"content":""},"done":true}`+"\n")
		flusher.Flush()
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "test-model", "sys", 8192, 200*time.Millisecond, retry.Config{}, slog.Default())

	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected success for a slow-but-steady stream, got error: %v", err)
	}
	want := "Hello world this is slow"
	if reply != want {
		t.Errorf("reply = %q, want %q", reply, want)
	}
}

func TestSendOllama_SilentStream_TimesOutButPreservesPartial(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"message":{"content":"partial answer"},"done":false}`+"\n")
		flusher.Flush()
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "test-model", "sys", 8192, 100*time.Millisecond, retry.Config{}, slog.Default())

	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected a timeout error for a stream that goes silent, got nil")
	}
	if reply != "partial answer" {
		t.Errorf("expected partial content to be preserved, got reply=%q err=%v", reply, err)
	}
}

func TestSendOllama_NoContentBeforeFailure_ReturnsNoPartial(t *testing.T) {
	t.Parallel()
	eng := NewLocalEngine("http://127.0.0.1:1", "test-model", "sys", 8192, 200*time.Millisecond, retry.Config{}, slog.Default())

	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error connecting to a closed port")
	}
	if reply != "" {
		t.Errorf("expected empty reply when nothing streamed, got %q", reply)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected a connection refused error, got: %v", err)
	}
}
