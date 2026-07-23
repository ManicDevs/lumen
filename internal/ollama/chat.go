// Package ollama provides a pure-Go client for the Ollama REST API. It
// covers chat completion, text generation, model management (list, pull,
// push, delete, copy, show, create), embeddings, loaded-model inspection,
// and server lifecycle (health checks, start/stop, version, blob storage).
// All types map directly to the Ollama JSON wire format.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Chat sends a chat completion request and returns the full response.
// The request's Stream field is ignored — use ChatStream for streaming.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	return c.chat(ctx, req, nil)
}

// ChatStream sends a chat completion request with streaming enabled. The
// callback fn is called for each ChatStreamChunk as it arrives. If fn
// returns an error, streaming stops and that error is returned.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, fn func(ChatStreamChunk) error) (ChatResponse, error) {
	if fn == nil {
		return c.chat(ctx, req, nil)
	}
	req.Stream = true
	return c.chat(ctx, req, fn)
}

func (c *Client) chat(ctx context.Context, req ChatRequest, fn func(ChatStreamChunk) error) (ChatResponse, error) {
	req.Stream = fn != nil

	var final ChatResponse
	var partial strings.Builder

	err := c.doStream(ctx, http.MethodPost, chatEndpoint, req, func(data []byte) error {
		var chunk ChatStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("ollama: chat decode chunk: %w", err)
		}
		partial.WriteString(chunk.Message.Content)
		if fn != nil {
			if err := fn(chunk); err != nil {
				return err
			}
		}
		if chunk.Done {
			final = ChatResponse{
				Message:            Message{Role: "assistant", Content: partial.String()},
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
		final = ChatResponse{
			Message: Message{Role: "assistant", Content: partial.String()},
			Done:    true,
		}
	}
	if final.Message.Content == "" {
		return final, errors.New("ollama: empty chat response")
	}
	return final, nil
}
