# Lumen — Agent Instructions

This file is referenced by `opencode.json` and provides instructions for AI agents working on the Lumen codebase.

## Project Overview

Lumen is a zero-dependency, pure-Go LLM code intelligence engine. It embeds a native Ollama REST API client with no external dependencies.

## Architecture

- **Entry point**: `cmd/lumen/main.go` (11 lines, delegates to `app.Run`)
- **All code**: `internal/` packages (14 packages, ~6000 lines)
- **Zero external dependencies** — Go stdlib only

## Key Constraints

1. **Never add external dependencies.** Use Go stdlib only.
2. **All errors must use `%w` wrapping.** Never use `%s` or `%v` for errors.
3. **All exported symbols must have godoc comments.**
4. **All new code must have tests.** Use table-driven tests with subtests.
5. **Use `t.Parallel()` only when safe** (no global state mutation, no `os.Chdir`, no `t.Setenv`).
6. **Run `go fmt -s -w .`** before committing.

## Testing

```bash
make test          # all tests
make race          # race detector
make test-3x       # 3 iterations to catch flaky tests
make cover         # HTML coverage
make fuzz          # fuzz tests (run for 30s minimum)
```

## Common Patterns

### Error handling
```go
if err != nil {
    return fmt.Errorf("context: %w", err)
}
```

### Table-driven tests
```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    int
        wantErr bool
    }{
        {"empty", "", 0, true},
        {"valid", "hello", 5, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got, err := DoSomething(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got %v, want %v", got, tt.want)
            }
        })
    }
}
```

### HTTP mocks (httptest)
```go
func TestFetch(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
    }))
    defer srv.Close()
    // use srv.URL
}
```

## Commit Convention

All commits must follow conventional format:
```
type(scope): description

Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert, deps
Scope: optional, lowercase
Description: imperative mood, lowercase, no period, <72 chars
```

## Package Responsibilities

| Package | Purpose |
|---------|---------|
| `internal/agent` | Autonomous agent loop, file/run parsing |
| `internal/app` | CLI dispatch, flag parsing, mode routing |
| `internal/audit` | Audit log with thread-safe JSONL storage |
| `internal/config` | Env-based config with validation |
| `internal/dataset` | Self-play dataset generation and training |
| `internal/env` | .env file loader |
| `internal/harvest` | Source code harvesting and minification |
| `internal/llm` | LLM engine interface (Ollama, OpenAI) |
| `internal/ollama` | Pure-Go Ollama REST API client |
| `internal/output` | Terminal formatting, spinner, logging |
| `internal/retry` | Exponential backoff retry |
| `internal/session` | Conversation history, audit trail |
| `internal/version` | Build-time version injection |
