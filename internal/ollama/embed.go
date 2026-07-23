package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Embed generates vector embeddings for the given inputs.
func (c *Client) Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error) {
	var resp EmbedResponse
	httpResp, err := c.doRequest(ctx, http.MethodPost, embedEndpoint, req)
	if err != nil {
		return resp, fmt.Errorf("ollama: embed: %w", err)
	}
	defer httpResp.Body.Close()
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return resp, fmt.Errorf("ollama: decode embed response: %w", err)
	}
	return resp, nil
}

// Ps lists currently loaded models on the server.
func (c *Client) Ps(ctx context.Context) (PsResponse, error) {
	var resp PsResponse
	httpResp, err := c.doRequest(ctx, http.MethodGet, psEndpoint, nil)
	if err != nil {
		return resp, fmt.Errorf("ollama: ps: %w", err)
	}
	defer httpResp.Body.Close()
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return resp, fmt.Errorf("ollama: decode ps response: %w", err)
	}
	return resp, nil
}
