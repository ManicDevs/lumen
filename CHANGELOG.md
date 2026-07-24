# Changelog

All notable changes to Lumen will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versioning follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `internal/version/` — build-time version injection via `-ldflags`
- `--version` flag to print version and exit
- `internal/ollama/example_test.go` — runnable godoc examples
- `internal/retry/example_test.go` — runnable godoc examples
- `.golangci.yml` — enterprise lint configuration (v2 format)
- `Makefile` — 60+ targets: build, test, lint, race, coverage, fuzz, bench, profile, Docker, release
- `CONTRIBUTING.md` — contributor guide
- `CHANGELOG.md` — release history

### Build & Infrastructure
- Multi-stage `Dockerfile` (alpine:3.20, non-root user, ~6MB binary)
- `.dockerignore` for minimal build context
- `docker-compose.yml` with Ollama + Lumen services
- `.goreleaser.yml` — multi-platform binaries, checksums, Docker multi-arch, GitHub Releases
- `.github/workflows/ci.yml` — enhanced CI: matrix builds, Go version matrix, security audit, coverage
- `.github/workflows/release.yml` — automated releases on `v*` tags
- `.github/dependabot.yml` — weekly gomod + github-actions updates
- `.github/CODEOWNERS` — code ownership rules
- `.github/ISSUE_TEMPLATE/bug_report.yml` — structured bug reports
- `.github/ISSUE_TEMPLATE/feature_request.yml` — structured feature requests
- `.github/PULL_REQUEST_TEMPLATE.md` — PR checklist
- `LICENSE` — MIT license

### Git Hooks
- `.githooks/pre-commit` — gofmt, go vet, golangci-lint, secret detection, go mod tidy
- `.githooks/pre-push` — full test suite before push
- `.githooks/commit-msg` — conventional commit format validation

### Testing
- Fuzz tests: `parser_fuzz_test.go`, `dotenv_fuzz_test.go`, `redact_fuzz_test.go`
- Benchmarks: `harvest_bench_test.go`, `ollama_bench_test.go`, `session_bench_test.go`
- Concurrency stress tests: `harvest_concurrent_test.go`, `ollama_extra_test.go`
- Integration tests: `app_run_test.go` with mock Ollama server
- Coverage: config 91%, llm 91%, audit 90%, ollama 85%, overall 84%

### Code Quality
- `golangci-lint` v2 migration with formatters section
- Removed broken double-check locking in `commentRegexCache`
- Audit log error propagation fixed
- All `fmt.Errorf` calls use `%w` for proper error wrapping
- `gofmt -s` formatting fixes across codebase

### Documentation
- `AGENTS.md` — AI agent instructions (referenced in opencode.json)
- `ANALYSIS.md` — 654-line codebase audit (47 findings)
- `SECURITY.md` — security policy and vulnerability reporting

### Changed
- Binary builds to `bin/lumen`; `/bin/`, `/build/`, `/dist/` in `.gitignore`
- `PrintUsage()` now includes `--version` flag
- All command references use `./bin/lumen` path
- Tests that mutate globals (`TTY`, `colorEnabled`) do not use `t.Parallel()`
