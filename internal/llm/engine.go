package llm

import "context"

type ChatMessage struct {
	Role    string
	Content string
}

type StreamFunc func(token string)

type Engine interface {
	Name() string
	Send(ctx context.Context, history []ChatMessage, onToken StreamFunc) (string, error)
}
