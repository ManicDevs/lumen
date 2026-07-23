package session

import (
	"fmt"
	"strings"
	"sync"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

type History struct {
	mu       sync.RWMutex
	messages []llm.ChatMessage
}

func NewHistory(initialContext string) *History {
	return &History{
		messages: []llm.ChatMessage{
			{Role: "user", Content: initialContext},
			{Role: "model", Content: "Indexed."},
		},
	}
}

func (h *History) Append(msg llm.ChatMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
}

func (h *History) Snapshot() []llm.ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]llm.ChatMessage, len(h.messages))
	copy(out, h.messages)
	return out
}

func (h *History) Render() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var b strings.Builder
	for _, m := range h.messages {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}
		b.WriteString(fmt.Sprintf("### %s\n\n%s\n\n", strings.ToUpper(role), m.Content))
	}
	return b.String()
}
