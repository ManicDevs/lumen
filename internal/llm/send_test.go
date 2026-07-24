package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

// --- OpenAI Send Tests ---

func TestOpenAI_Send_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hello"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":" World"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "test-model", "sys prompt", 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	history := []ChatMessage{{Role: "user", Content: "hi"}}

	var tokens []string
	reply, err := eng.Send(context.Background(), history, func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "Hello World" {
		t.Errorf("reply = %q, want %q", reply, "Hello World")
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestOpenAI_Send_Non200_Retriable(t *testing.T) {
	t.Parallel()
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintln(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for 429")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestOpenAI_Send_Non200_Permanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":{"message":"bad request"}}`)
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 3}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for 400")
	}
}

func TestOpenAI_Send_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestOpenAI_Send_ModelRoleMapping(t *testing.T) {
	t.Parallel()
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	history := []ChatMessage{
		{Role: "model", Content: "model says hi"},
		{Role: "user", Content: "hello"},
	}
	_, err := eng.Send(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Check that "model" role was mapped to "assistant" in the request
	msgs, ok := receivedBody["messages"].([]any)
	if !ok {
		t.Fatal("no messages in request body")
	}
	// First message is system, second should be "assistant" (mapped from "model")
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	secondMsg := msgs[1].(map[string]any)
	if secondMsg["role"] != "assistant" {
		t.Errorf("expected role 'assistant', got %q", secondMsg["role"])
	}
}

func TestOpenAI_Send_Non200_500_Retriable(t *testing.T) {
	t.Parallel()
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"error":{"message":"server error"}}`)
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for 500")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestOpenAI_Send_PartialContentOnError(t *testing.T) {
	t.Parallel()
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts == 1 {
			// Return partial content then close connection (simulates stream interruption)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"partial"}}]}`)
			w.(http.Flusher).Flush()
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
		} else {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"recovered"}}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		}
	}))
	defer srv.Close()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Logf("Send returned: reply=%q err=%v", reply, err)
	}
	if reply == "" {
		t.Error("expected non-empty partial or full reply")
	}
}

func TestOpenAI_Send_ContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	eng := NewOpenAIEngine(srv.URL, "m", "sys", 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	_, err := eng.Send(ctx, []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// --- Ollama Send Tests ---

func TestOllama_Send_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"Hello"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":" World"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":""},"done":true}`)
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "test-model", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	history := []ChatMessage{{Role: "user", Content: "hi"}}

	var tokens []string
	reply, err := eng.Send(context.Background(), history, func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "Hello World" {
		t.Errorf("reply = %q, want %q", reply, "Hello World")
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestOllama_Send_ModelRoleMapping(t *testing.T) {
	t.Parallel()
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"ok"},"done":true}`)
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	history := []ChatMessage{
		{Role: "model", Content: "model says hi"},
		{Role: "user", Content: "hello"},
	}
	_, err := eng.Send(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	msgs, ok := receivedBody["messages"].([]any)
	if !ok {
		t.Fatal("no messages in request body")
	}
	// system + assistant (from model) + user = 3
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	secondMsg := msgs[1].(map[string]any)
	if secondMsg["role"] != "assistant" {
		t.Errorf("expected role 'assistant', got %q", secondMsg["role"])
	}
}

func TestOllama_Send_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":""},"done":true}`)
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestOllama_Send_ConnectionRefused(t *testing.T) {
	t.Parallel()
	eng := NewLocalEngine("http://localhost:1", "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

func TestOllama_Send_ContextCancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.(http.Flusher).Flush()
		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	eng := NewLocalEngine(srv.URL, "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	_, err := eng.Send(ctx, []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestOllama_Send_OnTokenCallback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"message":{"content":"a"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"b"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"c"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":""},"done":true}`)
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1}, slog.Default())
	var tokens []string
	reply, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != "abc" {
		t.Errorf("reply = %q, want %q", reply, "abc")
	}
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d", len(tokens))
	}
}

func TestOllama_Send_Non200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	eng := NewLocalEngine(srv.URL, "m", "sys", 8192, 5*time.Second, retry.Config{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, slog.Default())
	_, err := eng.Send(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("expected error for 500")
	}
}
