package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	return srv, NewClient(srv.URL)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeNDJSON(w http.ResponseWriter, chunks []string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher := w.(http.Flusher)
	for _, c := range chunks {
		fmt.Fprintln(w, c)
		flusher.Flush()
	}
}

// --- Chat (non-streaming) ---

func TestChat_NonStreaming(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/chat") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, ChatResponse{
			Message: Message{Role: "assistant", Content: "Hello!"},
			Done:    true,
		})
	}))
	defer srv.Close()

	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", resp.Message.Content)
	}
	if !resp.Done {
		t.Error("expected Done=true")
	}
}

// --- Chat (streaming) ---

func TestChat_Streaming(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !req.Stream {
			t.Error("expected Stream=true")
		}
		writeNDJSON(w, []string{
			`{"message":{"content":"Hello"},"done":false}`,
			`{"message":{"content":" world"},"done":false}`,
			`{"message":{"content":""},"done":true}`,
		})
	}))
	defer srv.Close()

	var tokens []string
	var mu sync.Mutex
	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(chunk ChatStreamChunk) error {
		mu.Lock()
		tokens = append(tokens, chunk.Message.Content)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", resp.Message.Content)
	}
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
}

// --- Chat stream callback stops early ---

func TestChat_StreamCallbackErrorStops(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w, []string{
			`{"message":{"content":"a"},"done":false}`,
			`{"message":{"content":"b"},"done":false}`,
			`{"message":{"content":"c"},"done":false}`,
			`{"message":{"content":""},"done":true}`,
		})
	}))
	defer srv.Close()

	_, err := client.ChatStream(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(chunk ChatStreamChunk) error {
		callCount++
		if callCount >= 2 {
			return fmt.Errorf("stop now")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "stop now") {
		t.Errorf("expected callback error, got: %v", err)
	}
}

// --- Generate (non-streaming) ---

func TestGenerate_NonStreaming(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, GenerateResponse{
			Response: "Generated text",
			Done:     true,
		})
	}))
	defer srv.Close()

	resp, err := client.Generate(context.Background(), GenerateRequest{
		Model:  "test-model",
		Prompt: "write something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != "Generated text" {
		t.Errorf("expected 'Generated text', got %q", resp.Response)
	}
}

// --- Generate (streaming) ---

func TestGenerate_Streaming(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w, []string{
			`{"response":"Hello","done":false}`,
			`{"response":" world","done":false}`,
			`{"response":"","done":true}`,
		})
	}))
	defer srv.Close()

	var tokens []string
	resp, err := client.GenerateStream(context.Background(), GenerateRequest{
		Model:  "test-model",
		Prompt: "write",
	}, func(chunk GenerateStreamChunk) error {
		tokens = append(tokens, chunk.Response)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", resp.Response)
	}
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
}

// --- List models ---

func TestList(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		writeJSON(w, ListResponse{
			Models: []Model{
				{Name: "model-a", Size: 1000},
				{Name: "model-b", Size: 2000},
			},
		})
	}))
	defer srv.Close()

	resp, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(resp.Models))
	}
	if resp.Models[0].Name != "model-a" {
		t.Errorf("expected 'model-a', got %q", resp.Models[0].Name)
	}
}

// --- Pull streaming ---

func TestPullStream(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w, []string{
			`{"status":"pulling manifest"}`,
			`{"status":"success"}`,
		})
	}))
	defer srv.Close()

	var statuses []string
	err := client.PullStream(context.Background(), PullRequest{Model: "test-model"}, func(chunk PullProgressChunk) error {
		statuses = append(statuses, chunk.Status)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d: %v", len(statuses), statuses)
	}
}

// --- Delete ---

func TestDelete(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.Delete(context.Background(), "test-model"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Copy ---

func TestCopy(t *testing.T) {
	t.Parallel()
	copied := false
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CopyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Source != "src-model" || req.Destination != "dst-model" {
			t.Errorf("unexpected copy args: %+v", req)
		}
		copied = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.Copy(context.Background(), "src-model", "dst-model"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !copied {
		t.Error("copy was not called")
	}
}

// --- Show ---

func TestShow(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ShowResponse{
			Modelfile:  "FROM llama3.2",
			Parameters: "temperature 0.7",
			Template:   "{{ .Prompt }}",
		})
	}))
	defer srv.Close()

	resp, err := client.Show(context.Background(), ShowRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Modelfile != "FROM llama3.2" {
		t.Errorf("expected 'FROM llama3.2', got %q", resp.Modelfile)
	}
}

// --- Create ---

func TestCreate(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.Create(context.Background(), CreateRequest{Model: "test-model", From: "base"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Embed ---

func TestEmbed(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, EmbedResponse{
			Model:      "test-model",
			Embeddings: [][]float64{{0.1, 0.2}, {0.3, 0.4}},
		})
	}))
	defer srv.Close()

	resp, err := client.Embed(context.Background(), EmbedRequest{
		Model: "test-model",
		Input: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
}

// --- Ps ---

func TestPs(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, PsResponse{
			Models: []LoadedModel{
				{Name: "loaded-model", SizeVRAM: 4096},
			},
		})
	}))
	defer srv.Close()

	resp, err := client.Ps(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Errorf("expected 1 loaded model, got %d", len(resp.Models))
	}
}

// --- Health check ---

func TestHealth(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	srvClient := client.Server()
	if err := srvClient.Health(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealth_FailsOnUnreachable(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	srvClient := client.Server()
	if err := srvClient.Health(context.Background()); err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- WaitForReady ---

func TestWaitForReady_AlreadyReady(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Server().WaitForReady(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Version ---

func TestVersion(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": "0.1.0"})
	}))
	defer srv.Close()

	v, err := client.Version(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "0.1.0" {
		t.Errorf("expected '0.1.0', got %q", v)
	}
}

// --- BlobExists ---

func TestBlobExists(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exists, err := client.BlobExists(context.Background(), "sha256:abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected blob to exist")
	}
}

func TestBlobExists_NotFound(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	exists, err := client.BlobExists(context.Background(), "sha256:nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected blob to not exist")
	}
}

// --- HTTP error handling ---

func TestAPIError(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "bad request"})
	}))
	defer srv.Close()

	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("expected HTTP 400 error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("expected 'bad request' in error, got: %v", err)
	}
}

func TestAPIServerError(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "internal error"})
	}))
	defer srv.Close()

	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 error, got: %v", err)
	}
}

// --- Context cancellation ---

func TestContextCancellation(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/x-ndjson")
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, `{"message":{"content":"token-%d"},"done":false}`+"\n", i)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Chat(ctx, ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// --- Client option ---

func TestWithHTTPClient(t *testing.T) {
	t.Parallel()
	customClient := &http.Client{}
	client := NewClient("http://localhost:11434", WithHTTPClient(customClient))
	if client.httpClient != customClient {
		t.Error("expected custom HTTP client")
	}
}

// --- BaseURL ---

func TestBaseURL(t *testing.T) {
	t.Parallel()
	client := NewClient("http://localhost:11434/")
	if client.BaseURL() != "http://localhost:11434" {
		t.Errorf("expected trimmed base URL, got %q", client.BaseURL())
	}
}

// --- FindExecutable (just returns empty or something, should not panic) ---

func TestFindExecutable_NoPanic(t *testing.T) {
	t.Parallel()
	client := NewClient("http://localhost:11434")
	_ = client.Server().FindExecutable()
}

// --- Empty chat response ---

func TestChat_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w, []string{
			`{"message":{"content":""},"done":true}`,
		})
	}))
	defer srv.Close()

	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error for empty response")
	}
}

// --- Generate empty response ---

func TestGenerate_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w, []string{
			`{"response":"","done":true}`,
		})
	}))
	defer srv.Close()

	_, err := client.Generate(context.Background(), GenerateRequest{
		Model:  "test-model",
		Prompt: "test",
	})
	if err == nil {
		t.Error("expected error for empty response")
	}
}
