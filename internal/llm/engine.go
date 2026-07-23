// Package llm defines the engine interface that Lumen uses to communicate
// with local LLM backends. Implementations wrap Ollama, OpenAI-compatible
// servers, or any other provider behind the Engine interface so that the
// agent, harvest, and session packages never deal with HTTP or JSON directly.
package llm

import "context"

// ChatMessage represents a single turn in a conversation history.
type ChatMessage struct {
	Role    string
	Content string
}

// StreamFunc is called incrementally as tokens are received from the engine.
// A nil StreamFunc means the caller does not require streaming.
type StreamFunc func(token string)

// Engine is the interface that wraps a local LLM backend.
type Engine interface {
	// Name returns a human-readable name for the backend (e.g. "Ollama").
	Name() string
	// Send submits a conversation history to the model and returns the
	// assistant's reply. onToken, if non-nil, is called for each token.
	Send(ctx context.Context, history []ChatMessage, onToken StreamFunc) (string, error)
}
