// Command lumen is a local-first AI coding assistant that operates on your
// filesystem. It connects to local LLM engines (Ollama, LM Studio, or any
// OpenAI-compatible endpoint) and provides interactive code review, chat,
// autonomous agent task execution, dataset generation for fine-tuning, and
// snapshot-based session management — all without sending data to a third
// party.
package main

import (
	"os"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
