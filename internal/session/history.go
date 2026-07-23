package session

import (
	"fmt"
	"strings"
	"sync"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

// History is a thread-safe, in-memory conversation history backed by a
// growable slice of ChatMessages with copy-on-read snapshot semantics.
type History struct {
	mu       sync.RWMutex
	messages []llm.ChatMessage
}

// NewHistory creates a history seeded with the initial user context and a
// placeholder assistant acknowledgement ("Indexed.").
func NewHistory(initialContext string) *History {
	return &History{
		messages: []llm.ChatMessage{
			{Role: "user", Content: initialContext},
			{Role: "model", Content: "Indexed."},
		},
	}
}

// Append adds a message to the end of the history.
func (h *History) Append(msg llm.ChatMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, msg)
}

// Snapshot returns a deep copy of the entire message history. The caller
// may mutate the returned slice without affecting the History.
func (h *History) Snapshot() []llm.ChatMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]llm.ChatMessage, len(h.messages))
	copy(out, h.messages)
	return out
}

// Render formats the full history as a Markdown string with role headings.
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
