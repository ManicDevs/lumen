# Changelog

## [Unreleased]

### Added
- `internal/version/` — build-time version injection via `-ldflags`
- `--version` flag to print version and exit
- `internal/ollama/example_test.go` — runnable godoc examples
- `internal/retry/example_test.go` — runnable godoc examples
- `.golangci.yml` — enterprise lint configuration
- `.github/workflows/ci.yml` — GitHub Actions pipeline (lint, build, vet, test, race, coverage)
- `Makefile` — build/test/lint/race/cover/clean targets
- `CONTRIBUTING.md` — contributor guide
- `CHANGELOG.md` — release history

### Changed
- Binary builds to `bin/lumen`; `/bin/` added to `.gitignore`
- `PrintUsage()` now includes `--version` flag
- README: all command references use `./bin/lumen` path
- README: added proof section showing native pure-Go Ollama client output
- `cmd/lumen/main.go` — removed `flag` import, `--version` handled in `app.ParseFlags`
- All `fmt.Errorf` calls use `%w` for proper error wrapping
