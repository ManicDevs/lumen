package session

import (
	"testing"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
)

func BenchmarkHistory_Append(b *testing.B) {
	h := NewHistory("initial context")
	msg := llm.ChatMessage{Role: "user", Content: "hello world"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Append(msg)
	}
}

func BenchmarkHistory_Snapshot(b *testing.B) {
	h := NewHistory("initial context")
	for i := 0; i < 100; i++ {
		h.Append(llm.ChatMessage{Role: "user", Content: "message"})
		h.Append(llm.ChatMessage{Role: "assistant", Content: "response"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Snapshot()
	}
}

func BenchmarkHistory_Render(b *testing.B) {
	h := NewHistory("initial context")
	for i := 0; i < 50; i++ {
		h.Append(llm.ChatMessage{Role: "user", Content: "a longer user message that would be typical in a conversation"})
		h.Append(llm.ChatMessage{Role: "assistant", Content: "a longer assistant response that includes some code and explanation"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Render()
	}
}

func BenchmarkApproxTokens(b *testing.B) {
	inputs := []string{
		"",
		"short",
		"a medium length string with some tokens",
		"a very long string that repeats many times to simulate a large context window " +
			"that would be typical in a code review scenario with lots of source code " +
			"and discussion back and forth between the user and the assistant",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range inputs {
			_ = ApproxTokens(s)
		}
	}
}
