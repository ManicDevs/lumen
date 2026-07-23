# Contributing

## Prerequisites

- Go 1.25+
- `make`, `golangci-lint` (optional, for linting)
- An Ollama server running at `http://localhost:11434`

## Quick Start

```bash
make build   # bin/lumen
make test    # all tests
make race    # tests with race detector
```

## Code Style

- `go fmt ./...` before committing
- All exported symbols must have godoc comments
- Use `%w` in `fmt.Errorf` to wrap errors
- Zero external dependencies — stdlib only

## Project Layout

```
cmd/lumen/          — binary entrypoint
internal/
  agent/            — autonomous agent loop
  app/              — CLI dispatch, flag parsing
  config/           — env-based config loader
  dataset/          — self-play dataset generation
  env/              — .env file loader
  harvest/          — source code harvester
  llm/              — LLM engine interface (Ollama, OpenAI)
  ollama/           — pure-Go Ollama REST API client
  output/           — terminal formatting, spinner, logging
  retry/            — exponential backoff retry
  session/          — conversation history, audit log
  version/          — build-time version injection
```

## Testing

- `make test` — run all tests
- `make race` — run with race detector
- `make cover` — generate HTML coverage report
- Integration tests that require a live Ollama server are skipped with `-short`

## Pull Requests

1. `make vet && make test && make race` must pass
2. New code must include tests
3. Keep the diff focused — one feature/fix per PR

## Releases

Tags are created manually and built via CI:

```bash
git tag v0.1.0
git push origin v0.1.0
```
