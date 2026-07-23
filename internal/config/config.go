// Package config loads and validates Lumen's runtime configuration from
// environment variables, with a .env file as a lower-priority fallback.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/env"
)

// ---------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------

const (
	// Local
	DefaultOllamaHost    = "http://localhost:11434"
	DefaultOllamaModel   = "qwen2.5-coder:3b"
	DefaultOllamaNumCtx  = 8192
	DefaultLMStudioHost  = "http://localhost:1234"
	DefaultLMStudioModel = "local-model"
	DefaultLocalHost     = "" // generic OpenAI-compat; disabled if empty

	// Runtime
	DefaultRequestTimeout = 60 * time.Second
	DefaultMaxRetries     = 4
	DefaultLogFormat      = "text"
	DefaultLogLevel       = "info"
)

const SystemPrompt = "You are a senior software engineer reviewing source code for bugs. " +
	"The full text of one or more source files has already been provided to you as " +
	"context in this conversation, marked with '--- TARGET FILE IDENTIFIER ---' or " +
	"'--- SOURCE FILE ELEMENT ---' headers. Treat that as the actual code you are " +
	"looking at — never claim you are unable to inspect, view, or access the files, " +
	"and never ask the user to paste code that has already been given to you above. " +
	"Analyze the provided code directly and specifically for logic errors, edge " +
	"cases, security issues, and correctness problems, citing exact function and " +
	"variable names from it. Always respond in Markdown, placing any code in fenced " +
	"code blocks."

// ---------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------

type Config struct {
	// Local backends — Ollama is the primary/default engine; LMStudioHost
	// and LocalHost are alternate OpenAI-compat local servers a user can
	// point at instead.
	OllamaHost    string
	OllamaModel   string
	OllamaNumCtx  int
	LMStudioHost  string
	LMStudioModel string
	LocalHost     string // generic OpenAI-compat; disabled if empty
	LocalModel    string

	// Runtime
	SystemPrompt   string
	RequestTimeout time.Duration
	MaxRetries     int
	LogFormat      string
	LogLevel       string

	// Session
	SessionDir   string
	BackupDir    string
	AuditLogPath string
}

// Load builds a Config, applying .env values for any key not already set
// to a non-empty value in the real process environment.
func Load(logger *slog.Logger) (Config, error) {
	envPath := findDotEnv()
	warnOnLooseDotEnvPerms(envPath, logger)

	parsed, err := env.LoadDotenv(envPath)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	get := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v, ok := parsed[key]; ok && v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		// --- Local backends ---
		OllamaHost:    get("OLLAMA_HOST", DefaultOllamaHost),
		OllamaModel:   get("OLLAMA_MODEL", DefaultOllamaModel),
		OllamaNumCtx:  DefaultOllamaNumCtx,
		LMStudioHost:  get("LMSTUDIO_HOST", DefaultLMStudioHost),
		LMStudioModel: get("LMSTUDIO_MODEL", DefaultLMStudioModel),
		LocalHost:     get("LOCAL_HOST", DefaultLocalHost),
		LocalModel:    get("LOCAL_MODEL", ""),

		// --- Runtime ---
		SystemPrompt:   SystemPrompt,
		RequestTimeout: DefaultRequestTimeout,
		MaxRetries:     DefaultMaxRetries,
		LogFormat:      get("LOG_FORMAT", DefaultLogFormat),
		LogLevel:       get("LOG_LEVEL", DefaultLogLevel),
	}

	if v := get("REQUEST_TIMEOUT_SECONDS", ""); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			cfg.RequestTimeout = time.Duration(secs) * time.Second
		}
	}
	if v := get("MAX_RETRIES", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			cfg.MaxRetries = n
		}
	}
	// OLLAMA_NUM_CTX lets people running smaller/quantized local models (or
	// GPU-memory-constrained setups) lower the context window explicitly.
	// Requesting more context than a model/host can actually back can
	// silently fail (e.g. an empty generation) rather than erroring
	// clearly, so this was previously impossible to work around.
	if v := get("OLLAMA_NUM_CTX", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.OllamaNumCtx = n
		}
	}

	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".lumen", "sessions")
	cfg.SessionDir = get("SESSION_DIR", base)
	cfg.BackupDir = filepath.Join(cfg.SessionDir, "snapshots")
	cfg.AuditLogPath = filepath.Join(cfg.SessionDir, "audit.jsonl")

	return cfg, nil
}

// Validate returns an error if any required or malformed values are present.
func (c Config) Validate() error {
	if c.OllamaModel == "" {
		return fmt.Errorf("OLLAMA_MODEL must not be empty")
	}
	if c.MaxRetries < 1 {
		return fmt.Errorf("MAX_RETRIES must be >= 1")
	}
	if c.OllamaNumCtx < 1 {
		return fmt.Errorf("OLLAMA_NUM_CTX must be >= 1")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("REQUEST_TIMEOUT_SECONDS must be positive")
	}
	return nil
}

func findDotEnv() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ".env"
}

func warnOnLooseDotEnvPerms(path string, logger *slog.Logger) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 != 0 && logger != nil {
		logger.Warn("'.env' file is readable by group/other; consider chmod 600",
			"path", path, "mode", info.Mode().Perm().String())
	}
}
