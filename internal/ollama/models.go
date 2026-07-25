package ollama

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// List returns all models available on the Ollama server.
func (c *Client) List(ctx context.Context) (ListResponse, error) {
	var resp ListResponse
	httpResp, err := c.doRequest(ctx, http.MethodGet, tagsEndpoint, nil)
	if err != nil {
		return resp, fmt.Errorf("ollama: list models: %w", err)
	}
	defer httpResp.Body.Close()
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return resp, fmt.Errorf("ollama: decode list response: %w", err)
	}
	return resp, nil
}

// Pull downloads a model.
func (c *Client) Pull(ctx context.Context, req PullRequest) error {
	if req.Model == "" {
		return fmt.Errorf("ollama: pull model: model name is required")
	}
	return c.pullPushStream(ctx, pullEndpoint, req, nil)
}

// PullStream downloads a model, calling fn with progress updates.
func (c *Client) PullStream(ctx context.Context, req PullRequest, fn func(PullProgressChunk) error) error {
	if req.Model == "" {
		return fmt.Errorf("ollama: pull model: model name is required")
	}
	if fn == nil {
		return c.pullPushStream(ctx, pullEndpoint, req, nil)
	}
	req.Stream = true
	return c.pullPushStream(ctx, pullEndpoint, req, fn)
}

// Push uploads a model.
func (c *Client) Push(ctx context.Context, req PushRequest) error {
	return c.pullPushStream(ctx, pushEndpoint, req, nil)
}

// PushStream uploads a model, calling fn with progress updates.
func (c *Client) PushStream(ctx context.Context, req PushRequest, fn func(PullProgressChunk) error) error {
	if fn == nil {
		return c.pullPushStream(ctx, pushEndpoint, req, nil)
	}
	req.Stream = true
	return c.pullPushStream(ctx, pushEndpoint, req, fn)
}

func (c *Client) pullPushStream(ctx context.Context, endpoint string, body any, fn func(PullProgressChunk) error) error {
	return c.doStream(ctx, http.MethodPost, endpoint, body, func(data []byte) error {
		var chunk PullProgressChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("ollama: decode progress: %w", err)
		}
		if fn != nil {
			return fn(chunk)
		}
		return nil
	})
}

// Delete removes a model from the server.
func (c *Client) Delete(ctx context.Context, model string) error {
	if model == "" {
		return fmt.Errorf("ollama: delete model: model name is required")
	}
	_, err := c.doRequest(ctx, http.MethodDelete, deleteEndpoint, DeleteRequest{Model: model})
	if err != nil {
		return fmt.Errorf("ollama: delete model %q: %w", model, err)
	}
	return nil
}

// Copy duplicates a model on the server under a new name.
func (c *Client) Copy(ctx context.Context, src, dst string) error {
	_, err := c.doRequest(ctx, http.MethodPost, copyEndpoint, CopyRequest{Source: src, Destination: dst})
	if err != nil {
		return fmt.Errorf("ollama: copy %q -> %q: %w", src, dst, err)
	}
	return nil
}

// Show returns detailed information about a model.
func (c *Client) Show(ctx context.Context, req ShowRequest) (ShowResponse, error) {
	var resp ShowResponse
	httpResp, err := c.doRequest(ctx, http.MethodPost, showEndpoint, req)
	if err != nil {
		return resp, fmt.Errorf("ollama: show model %q: %w", req.Model, err)
	}
	defer httpResp.Body.Close()
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return resp, fmt.Errorf("ollama: decode show response: %w", err)
	}
	return resp, nil
}

// CreateBlob uploads a file as a blob to Ollama, returns the digest.
func (c *Client) CreateBlob(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open blob file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash blob: %w", err)
	}
	digest := fmt.Sprintf("sha256:%x", h.Sum(nil))

	// Rewind file
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek blob: %w", err)
	}

	// Check if blob already exists
	httpResp, err := c.doRequest(ctx, http.MethodHead, fmt.Sprintf("%s/%s", blobsEndpoint, digest), nil)
	if err == nil && httpResp.StatusCode == http.StatusOK {
		return digest, nil
	}
	if httpResp != nil {
		httpResp.Body.Close()
	}

	// Upload blob
	f.Seek(0, io.SeekStart)
	stat, _ := f.Stat()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+blobsEndpoint, f)
	if err != nil {
		return "", fmt.Errorf("create blob request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = stat.Size()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload blob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("blob upload failed: HTTP %d", resp.StatusCode)
	}

	return digest, nil
}

// Create builds a new model from a Modelfile (or from an existing model
// with a FROM directive). Supports files and LoRA adapters.
func (c *Client) Create(ctx context.Context, req CreateRequest) error {
	_, err := c.doRequest(ctx, http.MethodPost, createEndpoint, req)
	if err != nil {
		return fmt.Errorf("ollama: create model %q: %w", req.Model, err)
	}
	return nil
}

// CreateWithBlobs creates a model with model files and LoRA adapters.
// It uploads all files in req.Files and req.Adapters as blobs first.
func (c *Client) CreateWithBlobs(ctx context.Context, req CreateRequest) error {
	// Upload model files
	for name, path := range req.Files {
		digest, err := c.CreateBlob(ctx, path)
		if err != nil {
			return fmt.Errorf("upload model file %q: %w", name, err)
		}
		req.Files[name] = digest
	}

	// Upload LoRA adapters
	for name, path := range req.Adapters {
		digest, err := c.CreateBlob(ctx, path)
		if err != nil {
			return fmt.Errorf("upload adapter %q: %w", name, err)
		}
		req.Adapters[name] = digest
	}

	return c.Create(ctx, req)
}
