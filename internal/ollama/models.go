package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// Pull downloads a model. Use PullStream for progress callbacks.
func (c *Client) Pull(ctx context.Context, req PullRequest) error {
	return c.pullPushStream(ctx, pullEndpoint, req, nil)
}

// PullStream downloads a model, calling fn with progress updates. If fn
// returns an error the download is aborted.
func (c *Client) PullStream(ctx context.Context, req PullRequest, fn func(PullProgressChunk) error) error {
	if fn == nil {
		return c.pullPushStream(ctx, pullEndpoint, req, nil)
	}
	req.Stream = true
	return c.pullPushStream(ctx, pullEndpoint, req, fn)
}

// Push uploads a model. Use PushStream for progress callbacks.
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

// Show returns detailed information about a model (modelfile, parameters,
// template, and model metadata).
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

// Create builds a new model from a Modelfile (or from an existing model
// with a FROM directive).
func (c *Client) Create(ctx context.Context, req CreateRequest) error {
	_, err := c.doRequest(ctx, http.MethodPost, createEndpoint, req)
	if err != nil {
		return fmt.Errorf("ollama: create model %q: %w", req.Model, err)
	}
	return nil
}
