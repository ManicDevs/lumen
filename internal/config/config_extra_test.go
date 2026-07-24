package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWarnOnLooseDotEnvPerms_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only test")
	}
	warnOnLooseDotEnvPerms("any.path", slog.Default())
}

func TestWarnOnLooseDotEnvPerms_PermsOK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEY=value"), 0600); err != nil {
		t.Fatal(err)
	}
	warnOnLooseDotEnvPerms(path, slog.Default())
}

func TestLoad_WithDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("OLLAMA_MODEL=test-model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OllamaModel != "test-model" {
		t.Errorf("expected 'test-model', got %q", cfg.OllamaModel)
	}
}

func TestLoad_WithEnvOverrides(t *testing.T) {
	t.Setenv("OLLAMA_MODEL", "env-model")
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:9999")
	t.Setenv("OLLAMA_NUM_CTX", "4096")
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "120")
	t.Setenv("MAX_RETRIES", "2")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OllamaModel != "env-model" {
		t.Errorf("expected 'env-model', got %q", cfg.OllamaModel)
	}
	if cfg.OllamaNumCtx != 4096 {
		t.Errorf("expected 4096, got %d", cfg.OllamaNumCtx)
	}
}

func TestLoad_InvalidRequestTimeout_NotANumber(t *testing.T) {
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "not-a-number")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("expected default timeout, got %v", cfg.RequestTimeout)
	}
}

func TestLoad_InvalidMaxRetries_NotANumber(t *testing.T) {
	t.Setenv("MAX_RETRIES", "not-a-number")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected default max retries, got %d", cfg.MaxRetries)
	}
}

func TestLoad_InvalidNumCtx_NotANumber(t *testing.T) {
	t.Setenv("OLLAMA_NUM_CTX", "not-a-number")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OllamaNumCtx != DefaultOllamaNumCtx {
		t.Errorf("expected default num ctx, got %d", cfg.OllamaNumCtx)
	}
}

func TestLoad_InvalidNumCtx_Negative(t *testing.T) {
	t.Setenv("OLLAMA_NUM_CTX", "-1")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OllamaNumCtx != DefaultOllamaNumCtx {
		t.Errorf("expected default num ctx, got %d", cfg.OllamaNumCtx)
	}
}

func TestLoad_InvalidMaxRetries_Zero(t *testing.T) {
	t.Setenv("MAX_RETRIES", "0")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected default max retries, got %d", cfg.MaxRetries)
	}
}

func TestLoad_InvalidTimeout_Zero(t *testing.T) {
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "0")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("expected default timeout, got %v", cfg.RequestTimeout)
	}
}

func TestFindDotEnv_Override(t *testing.T) {
	t.Parallel()
	path := findDotEnv()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}
