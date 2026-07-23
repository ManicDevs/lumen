package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Generate sends a text generation (completion) request and returns the full
// response. For streaming, use GenerateStream.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	return c.generate(ctx, req, nil)
}

// GenerateStream sends a text generation request with streaming. The
// callback fn is called for each chunk as it arrives.
func (c *Client) GenerateStream(ctx context.Context, req GenerateRequest, fn func(GenerateStreamChunk) error) (GenerateResponse, error) {
	if fn == nil {
		return c.generate(ctx, req, nil)
	}
	req.Stream = true
	return c.generate(ctx, req, fn)
}

func (c *Client) generate(ctx context.Context, req GenerateRequest, fn func(GenerateStreamChunk) error) (GenerateResponse, error) {
	req.Stream = fn != nil

	var final GenerateResponse
	var partial strings.Builder

	err := c.doStream(ctx, http.MethodPost, generateEndpoint, req, func(data []byte) error {
		var chunk GenerateStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("ollama: generate decode chunk: %w", err)
		}
		partial.WriteString(chunk.Response)
		if fn != nil {
			if err := fn(chunk); err != nil {
				return err
			}
		}
		if chunk.Done {
			final = GenerateResponse{
				Response:           partial.String(),
				Done:               true,
				DoneReason:         chunk.DoneReason,
				TotalDuration:      chunk.TotalDuration,
				LoadDuration:       chunk.LoadDuration,
				PromptEvalCount:    chunk.PromptEvalCount,
				PromptEvalDuration: chunk.PromptEvalDuration,
				EvalCount:          chunk.EvalCount,
				EvalDuration:       chunk.EvalDuration,
			}
		}
		return nil
	})
	if err != nil {
		return final, err
	}
	if !final.Done && partial.Len() > 0 {
		final = GenerateResponse{
			Response: partial.String(),
			Done:     true,
		}
	}
	if final.Response == "" {
		return final, errors.New("ollama: empty generate response")
	}
	return final, nil
}
