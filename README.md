# Lumen — Local LLM Code Intelligence Engine

> **Zero-dependency, pure-Go framework with a fully native Ollama client
> baked in — no shell-outs, no SDKs, no cloud, no keys.**

---

## Why Lumen?

Every other AI coding tool relies on an external Ollama binary, a Python
runtime, or a cloud API. Lumen embeds its own **complete Ollama REST API
client** directly in the Go process — 450+ lines of purpose-built,
test-covered framework code in `internal/ollama/`.

| Capability | Lumen | Others |
|---|---|---|
| LLM backend client | **Pure-Go native** (`internal/ollama/`) | HTTP curl wrappers or external SDKs |
| Server management | **Built-in** — `Server.Start()`, `Stop()`, `Health()`, `WaitForReady()` | Manual `ollama serve` required |
| Model lifecycle | **Full CRUD** — `List`, `Pull`, `Push`, `Delete`, `Copy`, `Show`, `Create` | None or partial |
| Streaming | **Unified** `ChatStream` / `GenerateStream` with per-chunk callbacks | Varies per SDK |
| Error handling | **Structured** `apiError` with HTTP status + message extraction | Raw status-code switches |
| Dependencies | **Zero** — pure Go stdlib only | External SDKs, CGo, or shell commands |

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        cmd/lumen/main.go                         │
│                     (thin entrypoint, 11 lines)                   │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│  ┌──────────────────────┐  ┌──────────────────────────────────┐  │
│  │   internal/app/      │  │       internal/config/            │  │
│  │   • ParseFlags       │  │  • URL-validated OLLAMA_HOST      │  │
│  │   • Mode dispatch    │  │  • Range-checked NUM_CTX          │  │
│  │   • Signal handling  │  │  • Format/level validation        │  │
│  │   • Snapshot mgmt    │  │  • .env fallback                  │  │
│  └──────────┬───────────┘  └────────────────────────────────────┘  │
│             │                                                       │
│  ┌──────────▼───────────┐  ┌────────────────────────────────────┐  │
│  │  internal/agent/     │  │    internal/session/                │  │
│  │  • Iterative loop    │  │  • Thread-safe History              │  │
│  │  • Sandbox denylist  │  │  • JSONL AuditLog                   │  │
│  │  • File/run parsing  │  │  • Approx token counting            │  │
│  │  • Path-traversal    │  │                                     │  │
│  │    protection        │  └────────────────────────────────────┘  │
│  └──────────┬───────────┘                                           │
│             │                                                       │
│  ┌──────────▼───────────┐  ┌────────────────────────────────────┐  │
│  │   internal/llm/      │  │    internal/harvest/                │  │
│  │  • Engine interface  │  │  • Polyglot source harvesting       │  │
│  │  • LocalEngine       │  │  • Comment stripping (21 langs)    │  │
│  │    (wraps ollama)    │  │  • 16 MiB size limit                │  │
│  │  • OpenAIEngine      │  │  • Symlink-safe validation          │  │
│  │    (LM Studio, etc)  │  └────────────────────────────────────┘  │
│  └──────────┬───────────┘                                           │
│             │                                                       │
│  ┌──────────▼──────────────────────────────────────────────────┐   │
│  │               internal/ollama/ ★                             │   │
│  │   ┌─────────────┬──────────────┬──────────────┬──────────┐   │   │
│  │   │ Chat/Stream │ Generate/    │ Model CRUD   │ Server   │   │   │
│  │   │             │ Stream       │              │ Lifecycle│   │   │
│  │   ├─────────────┼──────────────┼──────────────┼──────────┤   │   │
│  │   │ Chat()      │ Generate()   │ List()       │ Server() │   │   │
│  │   │ ChatStream()│ GenerateStr. │ Pull()/Str.  │ Health() │   │   │
│  │   │             │              │ Push()/Str.  │ Start()  │   │   │
│  │   │             │              │ Delete()     │ Stop()   │   │   │
│  │   │             │              │ Copy()       │ WaitForR.│   │   │
│  │   │             │              │ Show()       │ Version()│   │   │
│  │   │             │              │ Create()     │ Blob*()  │   │   │
│  │   └─────────────┴──────────────┴──────────────┴──────────┘   │   │
│  │             ▲                                                 │   │
│  │    All HTTP + NDJSON handled internally                      │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌────────────────────────────────────────┐  ┌──────────────────┐  │
│  │      internal/output/                   │  │  internal/retry/ │  │
│  │  • slog logger (text/json)              │  │  • Exp. backoff  │  │
│  │  • ANSI styling (Bold/Dim/Cyan/Red)     │  │  • Jitter safety │  │
│  │  • Secret redaction                     │  │  • Permanent err │  │
│  │  • Terminal spinner                     │  │    detection     │  │
│  └────────────────────────────────────────┘  └──────────────────┘  │
│                             ┌──────────────────────┐               │
│                             │  internal/dataset/   │               │
│                             │  • Self-play gen     │               │
│                             │  • Git-like commits  │               │
│                             │  • Model fine-tuning │               │
│                             └──────────────────────┘               │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Project Layout

```
lumen/
├── cmd/lumen/main.go              # Entrypoint: 11 lines
├── internal/
│   ├── app/                       # Wiring + mode dispatch + signal handling
│   │   ├── app.go                 # Run(), REPL, snapshot helpers
│   │   └── flags.go               # Flag parsing + usage text
│   ├── config/                    # Config load + validation (URL, range, level)
│   ├── env/                       # .env parser (process env wins)
│   ├── llm/                       # Engine interface + adapters
│   │   ├── engine.go              # ChatMessage, StreamFunc, Engine interface
│   │   ├── ollama.go              # LocalEngine — wraps internal/ollama
│   │   ├── openai.go              # OpenAIEngine — generic compat backend
│   │   └── helpers.go             # API error formatting
│   ├── ollama/                    # ★ Pure-Go Ollama REST API framework
│   │   ├── client.go              # HTTP transport + streaming
│   │   ├── chat.go                # Chat completion
│   │   ├── generate.go            # Text generation
│   │   ├── models.go              # Model CRUD (list, pull, push, delete, …)
│   │   ├── embed.go               # Embeddings + ps
│   │   ├── server.go              # Lifecycle (start/stop/health/version)
│   │   ├── types.go               # All request/response types
│   │   └── ollama_test.go         # 27 tests (httptest-based)
│   ├── agent/                     # Autonomous coding agent
│   │   ├── agent.go               # Iterative Run() loop
│   │   ├── parser.go              # Fenced block parser
│   │   └── sandbox.go             # Sandbox + denylist + file writes
│   ├── harvest/                   # Source code harvesting
│   │   ├── harvest.go             # Context(), MinifyCode(), ValidateTargetPath()
│   │   └── languages.go           # 21 languages + test/pattern exclusion
│   ├── session/                   # Conversation history + audit log
│   │   ├── history.go             # Thread-safe History
│   │   └── audit.go               # Append-only JSONL audit trail
│   ├── dataset/                   # Dataset generation + training
│   │   ├── types.go               # Datapoint, Commit, RefPointer
│   │   ├── generate.go            # Self-play generation loop
│   │   ├── init.go                # Repository initialisation
│   │   └── train.go               # Model fine-tuning
│   ├── output/                    # Terminal output + logging
│   │   ├── logger.go              # slog setup (text/json)
│   │   ├── style.go               # ANSI styles + spinner
│   │   └── redact.go              # Secret scrubbing
│   └── retry/                     # Exponential backoff + jitter
├── data/datasets/                 # Git-like dataset repository
│   ├── commits/                   # Content-addressed commit files
│   ├── stage/                     # In-progress frame buffer
│   └── refs/heads/master          # Branch pointer
├── .env.example
├── go.mod                         # Go 1.25 — zero external dependencies
└── README.md
```

---

## Quick Start

```bash
# Build (no dependencies to download — just Go 1.25+)
go build -o lumen ./cmd/lumen

# Review a project (harvests source → interactive chat)
./lumen /path/to/your/project/

# Plain chat session
./lumen --chat

# Autonomous agent
./lumen --auto "add a health-check endpoint and run the tests" --live-output

# Self-play dataset generation
./lumen --chat --easter-egg --continuous --pipe-dataset

# Fine-tune from collected datasets
./lumen --train

# See all options
./lumen --help
```

---

## Modes

### Code Mode (`lumen <path>`)

Harvests a file or directory of source files (Go, Python, JS/TS, Rust, Java,
C/C++, and 15+ more — see `internal/harvest/languages.go`), strips comments,
and opens an interactive chat with the code as context.

```
$ ./lumen ./myproject/
Lumen Code Mode: harvested ./myproject

[Lumen]: (first exchange generated automatically)
> explain the authentication flow
...
```

### Chat Mode (`lumen --chat`)

A plain interactive chat with no file context. Supports `/auto <goal>` to
hand control to the autonomous agent.

### Auto Mode (`lumen --auto <goal>` [flags])

Go-direct autonomous mode (no REPL). The agent iterates until `AUTO_DONE` or
the iteration cap:

```bash
lumen --auto "refactor the config loader, 10 iterations" --live-output
```

| Flag | Effect |
|---|---|
| `--auto <goal>` | Enable auto mode with objective |
| `--live-output` | Stream LLM tokens to stdout in real time |
| `--auto-sandbox` | Enable sandbox restrictions (denylist + path confinement) |

### Dataset Mode (`lumen --easter-egg`, `lumen --train`)

Self-play data generation for building fine-tuning datasets:

```bash
lumen --dataset-init                         # create repo structure
lumen --easter-egg --pipe-dataset            # generate + commit frames
lumen --train                                # fine-tune from fresh commits
lumen --train-all                            # fine-tune from all commits
```

---

## The Native Ollama Framework

Every other Go project that talks to Ollama uses one of:

1. **Raw `net/http`** — repetitive status-code switches, no streaming
   abstraction, no type safety.
2. **`github.com/ollama/ollama/api`** — pulls in the entire Ollama codebase
   as a dependency (~50 MiB of vendored CGo + llama.cpp).

Lumen takes a third path: **a purpose-built, pure-Go, zero-dependency Ollama
client** in `internal/ollama/`.

### What it covers

```
API Endpoint          Lumen Method                   Streaming
────────────────────────────────────────────────────────────────
GET  /api/tags        Client.List()                  —
POST /api/chat        Client.Chat() / ChatStream()   ✅ NDJSON
POST /api/generate    Client.Generate() / GenStream()✅ NDJSON
POST /api/pull        Client.Pull() / PullStream()   ✅ NDJSON
POST /api/push        Client.Push() / PushStream()   ✅ NDJSON
DELETE /api/delete    Client.Delete()                 —
POST /api/copy        Client.Copy()                   —
POST /api/show        Client.Show()                   —
POST /api/create      Client.Create()                 —
POST /api/embed       Client.Embed()                  —
GET  /api/ps          Client.Ps()                     —
HEAD /                Server.Health()                 —
GET  /api/version     Client.Version()                —
HEAD /api/blobs/:dig  Client.BlobExists()             —
POST /api/blobs/:dig  Client.BlobCreate()             —
```

### Server lifecycle management

```go
// Start Ollama as a managed subprocess
srv := client.Server()
srv.Start(ctx, ServerStartOptions{LogWriter: os.Stderr})
defer srv.Stop()

// Wait for it to be ready
srv.WaitForReady(ctx)

// Health check
err := srv.Health(ctx)

// Find the binary on any OS
path := srv.FindExecutable()  // checks $OLLAMA_BIN, PATH, common locations
```

### Why this matters

- **No CGo.** Builds instantly, cross-compiles to any target, no external
  shared libraries.
- **No vendored llama.cpp.** The binary stays small (~8 MiB).
- **No HTTP boilerplate.** Every endpoint is a typed Go function with
  structured errors and context support.
- **Tests included.** 27 httptest-based tests cover every endpoint,
  streaming, error handling, and context cancellation — with zero external
  infrastructure.

---

## Enterprise Hardening

| Area | What Lumen does |
|---|---|
| **Config validation** | URL format check for `OLLAMA_HOST`, range-clamping for `NUM_CTX` [256, 131072], `MAX_RETRIES` [1, 100], `REQUEST_TIMEOUT` [1s, 1h], log format/level enum |
| **Nil safety** | Every exported entrypoint (`agent.Run`, `config.Load`, `session.OpenAuditLog`, etc.) guards against nil parameters |
| **Signal handling** | `signal.NotifyContext(SIGINT, SIGTERM)` in `app.Run()` — Ctrl+C cancels the root context cleanly |
| **Panic recovery** | `retry.Do` clamps jitter to `math.MaxInt64`; `harvest.MinifyCode` enforces `MaxFileSize` (16 MiB) |
| **Path traversal** | `resolveWritePath` verifies the resolved file target stays inside the working directory |
| **Command sandbox** | Denylist of 14+ destructive patterns (`sudo`, `rm -rf /`, fork bombs, pipe-to-shell, …), restricted env |
| **Idempotency** | `dataset.RunInit()` is safe to re-run; `session.History.Snapshot()` returns deep copies |
| **Audit trail** | Thread-safe, append-only JSONL audit log with timestamps, token counts, durations, and engine names |
| **Retry hardening** | Jitter overflow protection, input bounds validation, context-cancellation checks |
| **Zero dependencies** | `go.mod` = 3 lines. No CGo, no external packages, no vendored SDKs |

---

## Configuration

Copy `.env.example` to `.env` to override defaults. Real process
environment variables always take precedence.

| Variable | Default | Description |
|---|---|---|
| `OLLAMA_HOST` | `http://localhost:11434` | Ollama server address (validated as URL) |
| `OLLAMA_MODEL` | `qwen2.5-coder:3b` | Model tag |
| `OLLAMA_NUM_CTX` | `8192` | Context window [256, 131072] |
| `REQUEST_TIMEOUT_SECONDS` | `60` | Per-request timeout [1, 3600] |
| `MAX_RETRIES` | `4` | Attempts including first [1, 100] |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

---

## Tests

```bash
go test ./... -count=1
```

94 tests across 10 packages — every line of the Ollama framework, agent loop,
session management, config validation, dataset operations, env parsing,
output formatting, retry logic, and LLM engine adapters.

```bash
go vet ./...
gofmt -l .
```

CI runs build, vet, format check, and the full suite on every push.

---

## License

Internal tool — see repository license file.
