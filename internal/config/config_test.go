package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate_AcceptsGoodConfig(t *testing.T) {
	c := Config{
		OllamaHost:     DefaultOllamaHost,
		OllamaModel:    DefaultOllamaModel,
		OllamaNumCtx:   DefaultOllamaNumCtx,
		MaxRetries:     DefaultMaxRetries,
		RequestTimeout: DefaultRequestTimeout,
		LogFormat:      DefaultLogFormat,
		LogLevel:       DefaultLogLevel,
	}
	if err := c.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_RejectsEmptyOllamaModel(t *testing.T) {
	c := Config{
		OllamaHost:     DefaultOllamaHost,
		OllamaModel:    "",
		MaxRetries:     1,
		RequestTimeout: DefaultRequestTimeout,
	}
	if err := c.Validate(); err == nil {
		t.Error("expected error for empty OllamaModel")
	}
}

func TestValidate_RejectsZeroMaxRetries(t *testing.T) {
	c := Config{
		OllamaModel:    DefaultOllamaModel,
		MaxRetries:     0,
		RequestTimeout: DefaultRequestTimeout,
	}
	if err := c.Validate(); err == nil {
		t.Error("expected error for MaxRetries=0")
	}
}

func TestValidate_RejectsZeroOllamaNumCtx(t *testing.T) {
	c := Config{
		OllamaModel:    DefaultOllamaModel,
		OllamaNumCtx:   0,
		MaxRetries:     DefaultMaxRetries,
		RequestTimeout: DefaultRequestTimeout,
	}
	if err := c.Validate(); err == nil {
		t.Error("expected error for OllamaNumCtx=0")
	}
}

func TestLoad_OllamaNumCtxDefaultAndOverride(t *testing.T) {
	os.Unsetenv("OLLAMA_NUM_CTX")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.OllamaNumCtx != DefaultOllamaNumCtx {
		t.Errorf("expected default OllamaNumCtx=%d, got %d", DefaultOllamaNumCtx, cfg.OllamaNumCtx)
	}

	os.Setenv("OLLAMA_NUM_CTX", "2048")
	defer os.Unsetenv("OLLAMA_NUM_CTX")
	cfg, err = Load(nil)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.OllamaNumCtx != 2048 {
		t.Errorf("expected OLLAMA_NUM_CTX override to be 2048, got %d", cfg.OllamaNumCtx)
	}
}

func TestWarnOnLooseDotEnvPerms_NoPanicOnMissingFile(t *testing.T) {
	warnOnLooseDotEnvPerms(filepath.Join(t.TempDir(), "nonexistent.env"), nil)
}

func TestWarnOnLooseDotEnvPerms_TightPermsNoWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("X=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	warnOnLooseDotEnvPerms(path, nil)
}
