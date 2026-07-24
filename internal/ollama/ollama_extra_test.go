package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// --- Pull (non-streaming) ---

func TestPull(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/pull") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.Pull(context.Background(), PullRequest{Model: "test-model"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Push (non-streaming) ---

func TestPush(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/push") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.Push(context.Background(), PushRequest{Model: "test-model"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- PushStream ---

func TestPushStream(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeNDJSON(w, []string{
			`{"status":"uploading"}`,
			`{"status":"success"}`,
		})
	}))
	defer srv.Close()

	var statuses []string
	err := client.PushStream(context.Background(), PushRequest{Model: "test-model"}, func(chunk PullProgressChunk) error {
		statuses = append(statuses, chunk.Status)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}
}

// --- PushStream nil callback ---

func TestPushStream_NilCallback(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.PushStream(context.Background(), PushRequest{Model: "m"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- PullStream nil callback ---

func TestPullStream_NilCallback(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := client.PullStream(context.Background(), PullRequest{Model: "m"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- BlobCreate ---

func TestBlobCreate(t *testing.T) {
	t.Parallel()
	var receivedBody []byte
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/api/blobs/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	data := strings.NewReader("blob data here")
	if err := client.BlobCreate(context.Background(), "sha256:abc123", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(receivedBody) != "blob data here" {
		t.Errorf("unexpected body: %q", receivedBody)
	}
}

func TestBlobCreate_Error(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	err := client.BlobCreate(context.Background(), "sha256:abc", strings.NewReader("data"))
	if err == nil {
		t.Error("expected error for blob create failure")
	}
}

// --- parseAPIError edge cases ---

func TestParseAPIError_EmptyBody(t *testing.T) {
	t.Parallel()
	err := parseAPIError(400, []byte{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should contain status code, got: %v", err)
	}
}

func TestParseAPIError_NonJSON(t *testing.T) {
	t.Parallel()
	err := parseAPIError(500, []byte("plain text error"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "plain text error") {
		t.Errorf("error should contain body text, got: %v", err)
	}
}

func TestParseAPIError_JSONWithNoErrorField(t *testing.T) {
	t.Parallel()
	err := parseAPIError(422, []byte(`{"detail":"invalid"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseAPIError_LongBody(t *testing.T) {
	t.Parallel()
	longBody := strings.Repeat("x", 300)
	err := parseAPIError(500, []byte(longBody))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "...") {
		t.Error("long body should be truncated")
	}
}

// --- Server Stop ---

func TestServer_StopNilCmd(t *testing.T) {
	t.Parallel()
	srv := &Server{client: NewClient("http://localhost:1")}
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop on nil cmd should not error: %v", err)
	}
}

func TestServer_StopNilProcess(t *testing.T) {
	t.Parallel()
	srv := &Server{
		client: NewClient("http://localhost:1"),
		cmd:    exec.Command("echo"),
	}
	// cmd.Process is nil because we haven't called Start
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop on nil process should not error: %v", err)
	}
}

func TestServer_Cmd(t *testing.T) {
	t.Parallel()
	srv := &Server{client: NewClient("http://localhost:1")}
	if srv.Cmd() != nil {
		t.Error("Cmd should return nil when no cmd set")
	}
}

// --- FindExecutable with OLLAMA_BIN ---

func TestFindExecutable_WithEnv(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv
	// Set OLLAMA_BIN to a nonexistent path — should fall through to system candidates
	srv := &Server{client: NewClient("http://localhost:1")}
	t.Setenv("OLLAMA_BIN", "/nonexistent/binary")
	// On this system, /usr/local/bin/ollama may exist, so the function
	// will fall through and find it. The key assertion is that the
	// nonexistent OLLAMA_BIN path is NOT returned.
	result := srv.FindExecutable()
	if result == "/nonexistent/binary" {
		t.Error("should not return nonexistent OLLAMA_BIN path")
	}
}

// --- WaitForReady timeout ---

func TestWaitForReady_Timeout(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.Server().WaitForReady(ctx)
	if err == nil {
		t.Error("expected error for timeout")
	}
}

// --- Version error ---

func TestVersion_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.Version(context.Background())
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Version non-JSON ---

func TestVersion_NonJSON(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0.1.0"))
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

// --- BlobExists error ---

func TestBlobExists_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.BlobExists(context.Background(), "sha256:abc")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- List error ---

func TestList_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.List(context.Background())
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Create error ---

func TestCreate_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	err := client.Create(context.Background(), CreateRequest{Model: "m"})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Show error ---

func TestShow_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.Show(context.Background(), ShowRequest{Model: "m"})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Copy error ---

func TestCopy_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	err := client.Copy(context.Background(), "src", "dst")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Delete error ---

func TestDelete_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	err := client.Delete(context.Background(), "m")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Embed error ---

func TestEmbed_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.Embed(context.Background(), EmbedRequest{Model: "m", Input: []string{"hi"}})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Ps error ---

func TestPs_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.Ps(context.Background())
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Generate non-streaming error ---

func TestGenerate_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.Generate(context.Background(), GenerateRequest{Model: "m", Prompt: "hi"})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- GenerateStream error ---

func TestGenerateStream_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	_, err := client.GenerateStream(context.Background(), GenerateRequest{Model: "m", Prompt: "hi"}, func(chunk GenerateStreamChunk) error {
		return nil
	})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Pull error ---

func TestPull_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	err := client.Pull(context.Background(), PullRequest{Model: "m"})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Push error ---

func TestPush_Error(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1")
	err := client.Push(context.Background(), PushRequest{Model: "m"})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- Chat nil callback (non-streaming path) ---

func TestChatStream_NilCallback(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, ChatResponse{
			Message: Message{Role: "assistant", Content: "Hello!"},
			Done:    true,
		})
	}))
	defer srv.Close()

	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Content != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", resp.Message.Content)
	}
}

// --- Start: executable not found ---

func TestServer_Start_NoExecutable(t *testing.T) {
	// This test only works when ollama is NOT installed on the system,
	// because FindExecutable checks absolute paths like /usr/local/bin/ollama
	// which bypass PATH. We verify by checking if FindExecutable returns "".
	srv := &Server{client: NewClient("http://localhost:1")}
	exe := srv.FindExecutable()
	if exe == "" {
		// ollama not installed — Start should return an error
		ctx := context.Background()
		err := srv.Start(ctx, ServerStartOptions{})
		if err == nil {
			t.Error("expected error when executable not found")
		}
	} else {
		t.Skipf("ollama installed at %s, cannot test 'not found' path", exe)
	}
}

// --- Start: actual executable (skip if ollama not installed) ---

func TestServer_Start_NotAvailable(t *testing.T) {
	t.Parallel()
	srv := &Server{client: NewClient("http://localhost:1")}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := srv.Start(ctx, ServerStartOptions{})
	if err == nil {
		t.Log("ollama is available — test passed but Start succeeded")
		_ = srv.Stop()
	}
}

// --- doRequest with bad URL ---

func TestDoRequest_BadURL(t *testing.T) {
	t.Parallel()
	client := NewClient("http://[::1]:invalid")
	_, err := client.doRequest(context.Background(), http.MethodGet, "/api/tags", nil)
	if err == nil {
		t.Error("expected error for bad URL")
	}
}

// --- BlobCreate read error after 300 ---

func TestBlobCreate_ReadError(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	err := client.BlobCreate(context.Background(), "sha256:abc", strings.NewReader("data"))
	if err == nil {
		t.Error("expected error for blob create 400")
	}
}

// --- pullPushStream decode error ---

func TestPullStream_DecodeError(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{invalid json`)
	}))
	defer srv.Close()

	err := client.PullStream(context.Background(), PullRequest{Model: "m"}, func(chunk PullProgressChunk) error {
		return nil
	})
	if err == nil {
		t.Error("expected error for decode failure")
	}
}

// --- parseAPIError with JSON error field ---

func TestParseAPIError_JSONError(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]string{"error": "not found"})
	err := parseAPIError(404, body)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain message, got: %v", err)
	}
}

// --- BlobCreate with empty body ---

func TestBlobCreate_EmptyBody(t *testing.T) {
	t.Parallel()
	srv, client := newTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := client.BlobCreate(context.Background(), "sha256:empty", strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
