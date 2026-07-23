package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// MinimumInterval is the shortest duration between health checks during WaitForReady.
const MinimumInterval = 50 * time.Millisecond

// Server wraps the lifecycle of an external ollama serve subprocess. Use
// Client.Server() to obtain a Server linked to an existing Client.
type Server struct {
	client *Client
	cmd    *exec.Cmd
}

// Server returns a Server that can start, stop, and health-check the Ollama
// subprocess for this client.
func (c *Client) Server() *Server {
	return &Server{client: c}
}

// Health checks whether the Ollama server is reachable with a HEAD request.
func (s *Server) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.client.baseURL, nil)
	if err != nil {
		return fmt.Errorf("ollama: health check request: %w", err)
	}
	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama: server not reachable at %s: %w", s.client.baseURL, err)
	}
	resp.Body.Close()
	return nil
}

// WaitForReady polls the server health endpoint until it responds or the
// context is cancelled. The polling interval is MinimumInterval (50ms).
func (s *Server) WaitForReady(ctx context.Context) error {
	if err := s.Health(ctx); err == nil {
		return nil
	}
	ticker := time.NewTicker(MinimumInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ollama: server did not become ready: %w", ctx.Err())
		case <-ticker.C:
			if err := s.Health(ctx); err == nil {
				return nil
			}
		}
	}
}

// FindExecutable searches common system paths and the OLLAMA_BIN environment
// variable for the ollama binary. Returns "" if not found.
func (s *Server) FindExecutable() string {
	if exe := os.Getenv("OLLAMA_BIN"); exe != "" {
		if info, err := os.Stat(exe); err == nil && !info.IsDir() {
			return exe
		}
	}

	candidates := []string{"ollama"}
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Ollama.app/Contents/Resources/ollama",
			filepath.Join(os.Getenv("HOME"), ".ollama", "ollama"),
		)
	case "linux":
		candidates = append(candidates,
			"/usr/local/bin/ollama",
			"/usr/bin/ollama",
			filepath.Join(os.Getenv("HOME"), ".ollama", "ollama"),
		)
	case "windows":
		dir := os.Getenv("LOCALAPPDATA")
		if dir != "" {
			candidates = append(candidates, filepath.Join(dir, "Programs", "Ollama", "ollama.exe"))
		}
	}

	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	return ""
}

// ServerStartOptions configure how the Ollama serve subprocess is launched.
type ServerStartOptions struct {
	Env       []string    // extra environment variables
	LogWriter io.Writer   // where to redirect stdout/stderr (nil = discard)
}

// Start finds the ollama binary, starts it as a subprocess with "serve",
// and waits for it to become ready via WaitForReady.
func (s *Server) Start(ctx context.Context, opts ServerStartOptions) error {
	exe := s.FindExecutable()
	if exe == "" {
		return errors.New("ollama: executable not found - install Ollama or set OLLAMA_BIN")
	}

	cmd := exec.CommandContext(ctx, exe, "serve")
	cmd.Stdout = opts.LogWriter
	cmd.Stderr = opts.LogWriter
	cmd.Env = append(os.Environ(), opts.Env...)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ollama: start server: %w", err)
	}
	s.cmd = cmd
	return s.WaitForReady(ctx)
}

// Cmd returns the underlying exec.Cmd for the running server, or nil.
func (s *Server) Cmd() *exec.Cmd {
	return s.cmd
}

// Stop sends SIGINT to the server subprocess, waits up to 5 seconds, then
// sends SIGKILL if it has not exited. It is safe to call multiple times.
func (s *Server) Stop() error {
	if s.cmd == nil {
		return nil
	}
	if s.cmd.Process == nil {
		return nil
	}
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		s.cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() {
		s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.cmd.Process.Kill()
	}
	s.cmd = nil
	return nil
}

// Version returns the Ollama server version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	if err != nil {
		return "", fmt.Errorf("ollama: version request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: version: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &v) != nil {
		return strings.TrimSpace(string(body)), nil
	}
	return v.Version, nil
}

// BlobExists checks whether a blob with the given digest exists on the server.
// BlobExists checks whether a blob with the given SHA256 digest exists.
func (c *Client) BlobExists(ctx context.Context, digest string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.baseURL+"/api/blobs/"+digest, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// BlobCreate pushes a blob to the server.
// BlobCreate uploads a blob to the server. The data is streamed from the
// reader. digest must be the SHA256 hash in "sha256:..." format.
func (c *Client) BlobCreate(ctx context.Context, digest string, data io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/blobs/"+digest, data)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("ollama: blob create: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
