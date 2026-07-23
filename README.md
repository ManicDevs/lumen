# Lumen — LLM Code Diagnostic Engine

A professional-grade CLI code-review and chat assistant that runs entirely
against a local Ollama server — no cloud provider, no API keys, no
per-token billing, and nothing leaves your machine.

## Project Layout

```
lumen/
├── cmd/lumen/main.go              # CLI entrypoint / REPL
├── internal/
│   ├── config/                    # Config loading + validation
│   ├── dotenv/                    # .env file parser
│   ├── engine/
│   │   ├── types.go               # Engine interface + shared types
│   │   ├── openai_compat.go       # Shared OpenAI-compat wire types
│   │   │                          # (used by local.go's LM Studio path)
│   │   ├── local.go               # Ollama + LM Studio + generic OAI-compat
│   │   └── helpers.go             # Shared API error formatting
│   ├── harvest/                   # Source code minify + snapshot
│   ├── logging/                   # Structured slog setup
│   ├── redact/                    # Secret scrubbing for logs/errors/URLs
│   ├── retry/                     # Exponential backoff with jitter
│   └── session/                   # Conversation history + JSONL audit log
├── .env.example
└── README.md
```

## Build

```bash
go build -o lumen ./cmd/lumen
```

## Usage

```bash
./lumen --chat                  # plain chat, no code context
./lumen ./path/to/file.go       # harvest one file, then chat
./lumen ./path/to/project/      # harvest a directory of source files (polyglot)

./lumen --dataset-init          # git-init-style setup for data/datasets
./lumen --chat --easter-egg --continuous --pipe-dataset   # record commits
./lumen --train                 # fold fresh commits into a local model
```

Once inside the interactive shell (either mode), `/auto <goal>` hands control
to an autonomous agent loop — see [Autonomous agent mode](#autonomous-agent-mode-auto)
below.

### Synthetic dataset (`data/datasets`)

`--easter-egg --pipe-dataset` self-chains a local model against itself and,
on a clean finish, hashes the collected prompt/response pairs into a
content-addressed commit under `data/datasets/commits/`, advancing
`data/datasets/refs/heads/master` to point at it — a minimal, purpose-built
mirror of git's own commit/ref model (see `internal/engine/easter_egg.go`).
`--train` / `--train-all` fold those commits into a customized local Ollama
model (`internal/engine/trainer.go`).

`--dataset-init` sets that layout up explicitly and idempotently — creates
`commits/`, `stage/`, and `refs/heads/`, but (like a real `git init`) leaves
`refs/heads/master` itself unwritten until the first commit lands. Safe to
run before the first `--pipe-dataset` run, or again later — a second run
reports "Reinitialized" and never touches existing commits or refs.

### Autonomous agent mode (`/auto`)

From inside either Chat Mode or Code Mode's interactive shell, `/auto <goal>`
hands the conversation over to an autonomous loop that keeps prompting the
active engine, applying whatever it proposes, and reporting back, until the
model signals it's done or an iteration cap is hit:

```
> /auto add a health-check endpoint to the server, then run the tests
```

On each turn the loop looks for two kinds of fenced blocks in the model's
reply and acts on them directly:

- ` ```file:<path> ` ... ` ``` ` — write `<path>` (relative to the working
  directory) with the block's contents.
- ` ```run ` / ` ```sh ` / ` ```bash ` ... ` ``` ` — execute the block's
  contents as a shell command.

The model signals completion with `AUTO_DONE` (case-insensitive). A bare
`AUTO_DONE` with no prior file or command activity in the session is not
trusted at face value — the loop re-prompts once, asking the model to either
make the concrete change or explicitly confirm none is needed, before
accepting it.

**Flags:**

| Flag              | Effect                                                        |
|--------------------|----------------------------------------------------------------|
| `--auto-sandbox`   | Confine `/auto` to a restricted PATH/env and refuse a denylist of destructive-looking commands (see below). Off by default. |
| `--continuous` / `--autonomous` | Passed through as `continuous` for use alongside `--easter-egg`; unrelated to the iteration cap below. |

The default cap is `MaxIterations` (20) turns; append `N iterations` (or `max
N iterations`) to the goal to override it, e.g. `/auto refactor the config
loader, 40 iterations`.

**Sandbox mode (`--auto-sandbox`) — what it does and doesn't protect
against:**

- File writes are confined to the working directory: any target path is
  resolved and checked to be *inside* that directory (after resolving `..`
  traversal and rejecting anything that lands outside it) before being
  written. A literal `<...>`-style template placeholder echoed back by a
  weaker model is refused rather than written verbatim.
- Shell commands run with a minimal environment (`PATH`, `LANG`, `LC_ALL`,
  `HOME` only, falling back to a fixed safe `PATH` if none is set) and are
  checked against a denylist of destructive-looking shapes before running:
  `sudo`, `chmod`/`chown`, `shutdown`/`reboot`/`halt`/`poweroff`,
  `rm -rf`/`-fr`/`--recursive --force` targeting `/`, `~`, `$HOME`, or `*`,
  filesystem formatting (`mkfs`), raw disk writes (`dd if=...`, `> /dev/sd*`),
  fork bombs, piping `curl`/`wget` output straight into a shell, killing
  init, and overwriting `/etc/passwd`, `/etc/shadow`, or `/etc/sudoers`.
- **This is a denylist, not a security sandbox.** It blocks known-dangerous
  *shapes* of command, not arbitrary damage — a sufficiently different or
  obfuscated destructive command can still slip through, and outbound
  network access, reading arbitrary files, and non-destructive resource
  exhaustion are not restricted at all. Don't run `/auto --auto-sandbox`
  against anything you're not comfortable letting an unreviewed script touch,
  and don't treat `--auto-sandbox` as a substitute for running it in a
  disposable environment (container, VM, throwaway checkout) if the goal
  involves untrusted input.
- Without `--auto-sandbox`, `/auto` runs with your full environment and no
  command restrictions at all.

### Polyglot harvesting

Lumen isn't Go-only. Directory harvests recognize source files across many
languages (Go, Python, JS/TS, Java, C/C++, C#, Rust, Swift, Kotlin, Scala,
PHP, Ruby, shell, SQL, Lua, and more — see `internal/harvest/languages.go`
for the full list) and strip comments using the right token for each
language (`//`, `#`, `--`, etc.). Common test-file naming conventions
(`_test.go`, `.test.ts`, `.spec.ts`, `test_*.py`, `Test*.java`, `_spec.rb`)
and vendor/build/VCS directories (`node_modules`, `vendor`, `.git`, `dist`,
`build`, `venv`, `__pycache__`, `.idea`, `.vscode`) are excluded
automatically.

## Configuration

Copy `.env.example` to `.env` if you want to override any defaults. Real
shell `export` values always win over `.env`. Lumen needs no API keys —
just a reachable Ollama server.

### Ollama

| Variable        | Default                | Description                          |
|------------------|------------------------|---------------------------------------|
| OLLAMA_HOST      | http://localhost:11434 | Ollama server address                 |
| OLLAMA_MODEL     | qwen2.5-coder:3b        | Model tag to use (`ollama pull` first) |
| OLLAMA_NUM_CTX   | 8192                    | Context window size passed to Ollama  |

### Runtime

| Variable                  | Default  | Description                  |
|---------------------------|----------|------------------------------|
| LOG_FORMAT                | text     | text or json                 |
| LOG_LEVEL                 | info     | debug / info / warn / error  |
| REQUEST_TIMEOUT_SECONDS   | 60       | Per-request timeout          |
| MAX_RETRIES               | 4        | Attempts incl. first try     |

## Tests

```bash
go test ./...
```

`.github/workflows/ci.yml` runs `go build`, `go vet`, a `gofmt` check, and
the full test suite on every push and pull request against `master`/`main`.
# lumen
