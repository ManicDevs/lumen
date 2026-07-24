package ollama

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestFindExecutable_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()
	_ = s.FindExecutable()
}

func TestFindExecutable_OllamaBin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	dir := t.TempDir()
	fakeBin := dir + "/fake_ollama"
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_BIN", fakeBin)
	exe := s.FindExecutable()
	if exe != fakeBin {
		t.Errorf("expected %q, got %q", fakeBin, exe)
	}
}

func TestFindExecutable_OllamaBinIsDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	dir := t.TempDir()
	t.Setenv("OLLAMA_BIN", dir)
	// This hits the branch: stat succeeds, IsDir is true, so it falls through
	exe := s.FindExecutable()
	// The function should NOT return the directory path itself
	if exe == dir {
		t.Errorf("should not return directory path, got %q", exe)
	}
}

func TestFindExecutable_OllamaBinNonexistent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	t.Setenv("OLLAMA_BIN", "/nonexistent/path/to/ollama")
	// This hits the branch: stat fails, so it falls through
	exe := s.FindExecutable()
	if exe == "/nonexistent/path/to/ollama" {
		t.Errorf("should not return nonexistent path, got %q", exe)
	}
}

func TestVersion_NonJSONResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	version, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "not json" {
		t.Errorf("expected 'not json', got %q", version)
	}
}

func TestVersion_200JSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.9.0"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	version, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "0.9.0" {
		t.Errorf("expected '0.9.0', got %q", version)
	}
}

func TestVersion_ConnectionRefused(t *testing.T) {
	t.Parallel()
	c := NewClient("http://127.0.0.1:1")
	_, err := c.Version(context.Background())
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestBlobExists_ConnectionRefused(t *testing.T) {
	t.Parallel()
	c := NewClient("http://127.0.0.1:1")
	_, err := c.BlobExists(context.Background(), "sha256:abc123")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestBlobCreate_ErrorResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.BlobCreate(context.Background(), "sha256:abc123", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestBlobCreate_ReaderError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.BlobCreate(context.Background(), "sha256:abc123", &failingReader{})
	if err == nil {
		t.Fatal("expected error from failing reader")
	}
}

type failingReader struct{}

func (f *failingReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestHealth_Unreachable(t *testing.T) {
	t.Parallel()
	c := NewClient("http://127.0.0.1:1")
	s := c.Server()
	err := s.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestHealth_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	s := c.Server()
	err := s.Health(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestWaitForReady_TimeoutCtx(t *testing.T) {
	t.Parallel()
	c := NewClient("http://127.0.0.1:1")
	s := c.Server()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := s.WaitForReady(ctx)
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}

func TestCmd_NilServer(t *testing.T) {
	t.Parallel()
	s := &Server{}
	if s.Cmd() != nil {
		t.Error("expected nil cmd")
	}
}

func TestStart_NoBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	if path, _ := exec.LookPath("ollama"); path != "" {
		t.Skip("ollama binary found on system, cannot test no-binary path")
	}

	t.Setenv("OLLAMA_BIN", "")
	t.Setenv("PATH", "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := s.Start(ctx, ServerStartOptions{})
	if err == nil {
		t.Fatal("expected error when no executable found")
	}
}

func TestStart_InvalidBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	dir := t.TempDir()
	fakeBin := dir + "/not_ollama"
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexec sleep 999"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_BIN", fakeBin)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := s.Start(ctx, ServerStartOptions{})
	if err == nil {
		t.Fatal("expected error for non-executable binary")
	}
}

func TestStart_HealthCheckFails(t *testing.T) {
	// Use a client pointing at a non-existent URL so WaitForReady will fail
	c := NewClient("http://127.0.0.1:1")
	s := c.Server()

	dir := t.TempDir()
	fakeBin := dir + "/ollama_fake"
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nsleep 999"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_BIN", fakeBin)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := s.Start(ctx, ServerStartOptions{})
	if err == nil {
		s.Stop()
		t.Fatal("expected WaitForReady error when server doesn't start listening")
	}
}

func TestStop_RealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	s := &Server{cmd: cmd}
	if err := s.Stop(); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
	if s.cmd != nil {
		t.Error("expected cmd to be nil after Stop")
	}
}

func TestBlobCreate_Success2(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.BlobCreate(context.Background(), "sha256:abc123", strings.NewReader("data"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBlobExists_Success2(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	exists, err := c.BlobExists(context.Background(), "sha256:abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected blob to exist")
	}
}

func TestStop_ProcessNil(t *testing.T) {
	t.Parallel()
	s := &Server{}
	s.cmd = exec.Command("echo", "test")
	if err := s.Stop(); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestStop_SignalFails(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cmd.Process.Kill()
	cmd.Wait()

	s := &Server{cmd: cmd}
	err := s.Stop()
	if err != nil {
		t.Errorf("expected nil error from Stop on dead process, got: %v", err)
	}
}

func TestStart_CmdStartFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	// Use a non-executable file as OLLAMA_BIN — FindExecutable returns it
	// (stat succeeds, IsDir is false), but cmd.Start fails (permission denied)
	dir := t.TempDir()
	fakeBin := dir + "/not_executable"
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_BIN", fakeBin)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := s.Start(ctx, ServerStartOptions{})
	if err == nil {
		t.Fatal("expected error when cmd.Start fails")
	}
}

func TestFindExecutable_OllamaBinSymlink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(srv.URL)
	s := c.Server()

	dir := t.TempDir()
	realBin := dir + "/real_ollama"
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\nexit 0"), 0755); err != nil {
		t.Fatal(err)
	}
	symlink := dir + "/link_ollama"
	if err := os.Symlink(realBin, symlink); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_BIN", symlink)
	exe := s.FindExecutable()
	if exe != symlink {
		t.Errorf("expected %q, got %q", symlink, exe)
	}
}

func TestHealth_BadRequest(t *testing.T) {
	t.Parallel()
	c := NewClient("http://invalid-host-that-does-not-exist:9999")
	s := c.Server()
	err := s.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}

func TestVersion_BadRequest(t *testing.T) {
	t.Parallel()
	c := NewClient("http://invalid-host-that-does-not-exist:9999")
	_, err := c.Version(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}

func TestBlobExists_BadRequest(t *testing.T) {
	t.Parallel()
	c := NewClient("http://invalid-host-that-does-not-exist:9999")
	_, err := c.BlobExists(context.Background(), "sha256:abc")
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}

func TestBlobCreate_BadRequest(t *testing.T) {
	t.Parallel()
	c := NewClient("http://invalid-host-that-does-not-exist:9999")
	err := c.BlobCreate(context.Background(), "sha256:abc", strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error for invalid host")
	}
}

func TestWaitForReady_ImmediateReady(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	s := c.Server()
	err := s.WaitForReady(context.Background())
	if err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestVersion_ReadBodyError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Version(context.Background())
	if err == nil {
		t.Fatal("expected error for closed connection")
	}
}

func TestGenerateStream_NilCallback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunk := `{"response":"hello","done":true}`
		w.Write([]byte(chunk + "\n"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.GenerateStream(context.Background(), GenerateRequest{Model: "test", Prompt: "hi"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != "hello" {
		t.Errorf("expected 'hello', got %q", resp.Response)
	}
}

func TestGenerateStream_Callback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"response":"hel","done":false}`)
		fmt.Fprintln(w, `{"response":"lo","done":true}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	var chunks []string
	resp, err := c.GenerateStream(context.Background(), GenerateRequest{Model: "test", Prompt: "hi"}, func(chunk GenerateStreamChunk) error {
		chunks = append(chunks, chunk.Response)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != "hello" {
		t.Errorf("expected 'hello', got %q", resp.Response)
	}
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestGenerateStream_CallbackError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"response":"hello","done":true}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GenerateStream(context.Background(), GenerateRequest{Model: "test", Prompt: "hi"}, func(chunk GenerateStreamChunk) error {
		return io.ErrUnexpectedEOF
	})
	if err == nil {
		t.Fatal("expected error from callback")
	}
}

func TestGenerateStream_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"response":"","done":true}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GenerateStream(context.Background(), GenerateRequest{Model: "test", Prompt: "hi"}, nil)
	if err == nil {
		t.Fatal("expected error for empty response")
	}
}
