// Package engine implements Lumen's LLM backend behind a common interface.
// Lumen talks to local inference servers only — Ollama by default, or any
// OpenAI-compatible local server (e.g. LM Studio) — so nothing here ever
// leaves the machine.
package engine

import "context"

// ChatMessage is engine-agnostic. Role is "user", "assistant", or "model".
// Each backend translates it to its own wire format internally.
type ChatMessage struct {
	Role    string
	Content string
}

// StreamFunc is called with each token as it arrives so the caller can
// print progressive output without waiting for the full reply.
type StreamFunc func(token string)

// Engine is the interface every backend implements.
type Engine interface {
	// Name returns a short human-readable identifier for logs and UI.
	Name() string

	// Send sends the full conversation history to the backend, streaming
	// tokens via onToken as they arrive, and returns the complete text.
	// ctx governs timeout and cancellation including any internal retries.
	Send(ctx context.Context, history []ChatMessage, onToken StreamFunc) (string, error)
}
