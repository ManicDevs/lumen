package engine

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

// TestSendOllama_SlowButSteadyStream_DoesNotTimeOut reproduces the real
// production failure: a response that legitimately takes longer than the
// idle window to finish in total, but never actually goes silent for more
// than the idle window between chunks. The old flat http.Client.Timeout
// killed this at the total-duration mark even though the model was still
// actively producing tokens the whole time; the idle-watchdog approach
// must let it complete.
func TestSendOllama_SlowButSteadyStream_DoesNotTimeOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunks := []string{"Hello", " world", " this", " is", " slow"}
		for i, c := range chunks {
			fmt.Fprintf(w, `{"message":{"content":%q},"done":false}`+"\n", c)
			flusher.Flush()
			// Each individual gap is short; total exceeds a flat 200ms cap.
			time.Sleep(80 * time.Millisecond)
			_ = i
		}
		fmt.Fprint(w, `{"message":{"content":""},"done":true}`+"\n")
		flusher.Flush()
	}))
	defer srv.Close()

	// idleTimeout of 200ms: no single gap (80ms) exceeds it, even though
	// total stream duration (~400ms) would have exceeded an old-style flat
	// client timeout of the same value.
	eng := NewLocalEngine(BackendOllama, srv.URL, "test-model", "sys", 8192, 200*time.Millisecond, retry.Config{}, slog.Default())

	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("expected success for a slow-but-steady stream, got error: %v", err)
	}
	want := "Hello world this is slow"
	if reply != want {
		t.Errorf("reply = %q, want %q", reply, want)
	}
}

// TestSendOllama_SilentStream_TimesOutButPreservesPartial verifies a
// stream that actually goes silent (no chunk for longer than idleTimeout)
// still aborts, and that whatever content had already streamed is
// preserved on the returned error rather than discarded.
func TestSendOllama_SilentStream_TimesOutButPreservesPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, `{"message":{"content":"partial answer"},"done":false}`+"\n")
		flusher.Flush()
		// Go silent well past idleTimeout, never send done:true.
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	eng := NewLocalEngine(BackendOllama, srv.URL, "test-model", "sys", 8192, 100*time.Millisecond, retry.Config{}, slog.Default())

	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected a timeout error for a stream that goes silent, got nil")
	}
	if reply != "partial answer" {
		t.Errorf("expected partial content to be preserved, got reply=%q err=%v", reply, err)
	}
}

// TestSendOllama_NoContentBeforeFailure_ReturnsNoPartial confirms a
// connection-level failure before any content streams still returns an
// empty reply (nothing to preserve), matching prior behavior.
func TestSendOllama_NoContentBeforeFailure_ReturnsNoPartial(t *testing.T) {
	// Point at a host with nothing listening.
	eng := NewLocalEngine(BackendOllama, "http://127.0.0.1:1", "test-model", "sys", 8192, 200*time.Millisecond, retry.Config{}, slog.Default())

	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected an error connecting to a closed port")
	}
	if reply != "" {
		t.Errorf("expected empty reply when nothing streamed, got %q", reply)
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("expected a network error message, got: %v", err)
	}
}
