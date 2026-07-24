package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidate_HostEmpty(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.OllamaHost = ""
	if err := cfg.Validate(); err != errEmptyHost {
		t.Errorf("expected errEmptyHost, got %v", err)
	}
}

func TestValidate_HostInvalid(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.OllamaHost = "ftp://bad"
	if err := cfg.Validate(); err != errInvalidURL {
		t.Errorf("expected errInvalidURL, got %v", err)
	}
}

func TestValidate_HostNoScheme(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.OllamaHost = "localhost:11434"
	if err := cfg.Validate(); err != errInvalidURL {
		t.Errorf("expected errInvalidURL, got %v", err)
	}
}

func TestValidate_MaxRetriesZero(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.MaxRetries = 0
	if err := cfg.Validate(); err != errMaxRetries {
		t.Errorf("expected errMaxRetries, got %v", err)
	}
}

func TestValidate_MaxRetriesOver100(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.MaxRetries = 101
	if err := cfg.Validate(); err != errMaxRetries {
		t.Errorf("expected errMaxRetries, got %v", err)
	}
}

func TestValidate_NumCtxTooLow(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.OllamaNumCtx = 255
	if err := cfg.Validate(); err != errNumCtx {
		t.Errorf("expected errNumCtx, got %v", err)
	}
}

func TestValidate_NumCtxTooHigh(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.OllamaNumCtx = 131073
	if err := cfg.Validate(); err != errNumCtx {
		t.Errorf("expected errNumCtx, got %v", err)
	}
}

func TestValidate_TimeoutZero(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.RequestTimeout = 0
	if err := cfg.Validate(); err != errTimeout {
		t.Errorf("expected errTimeout, got %v", err)
	}
}

func TestValidate_TimeoutOverHour(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.RequestTimeout = 3601 * time.Second
	if err := cfg.Validate(); err != errTimeout {
		t.Errorf("expected errTimeout, got %v", err)
	}
}

func TestValidate_LogFormatInvalid(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.LogFormat = "xml"
	if err := cfg.Validate(); err != errLogFormat {
		t.Errorf("expected errLogFormat, got %v", err)
	}
}

func TestValidate_LogLevelInvalid(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.LogLevel = "verbose"
	if err := cfg.Validate(); err != errLogLevel {
		t.Errorf("expected errLogLevel, got %v", err)
	}
}

func TestValidateLogLevel_AllValid(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"debug", "info", "warn", "warning", "error", "DEBUG", "Info", "WARN"} {
		if err := validateLogLevel(level); err != nil {
			t.Errorf("validateLogLevel(%q) = %v", level, err)
		}
	}
}

func TestValidateLogLevel_Invalid(t *testing.T) {
	t.Parallel()
	if err := validateLogLevel("verbose"); err != errLogLevel {
		t.Errorf("expected errLogLevel, got %v", err)
	}
}

func TestValidateLogFormat_AllValid(t *testing.T) {
	t.Parallel()
	for _, fmt := range []string{"text", "json", "Text", "JSON"} {
		if err := validateLogFormat(fmt); err != nil {
			t.Errorf("validateLogFormat(%q) = %v", fmt, err)
		}
	}
}

func TestValidateLogFormat_Invalid(t *testing.T) {
	t.Parallel()
	if err := validateLogFormat("xml"); err != errLogFormat {
		t.Errorf("expected errLogFormat, got %v", err)
	}
}

func TestValidate_HostHTTPS(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.OllamaHost = "https://example.com"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid https host should pass, got %v", err)
	}
}

func TestValidate_BoundaryValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"min retries", func() Config { c := validConfig(); c.MaxRetries = 1; return c }(), nil},
		{"max retries", func() Config { c := validConfig(); c.MaxRetries = 100; return c }(), nil},
		{"min numCtx", func() Config { c := validConfig(); c.OllamaNumCtx = 256; return c }(), nil},
		{"max numCtx", func() Config { c := validConfig(); c.OllamaNumCtx = 131072; return c }(), nil},
		{"1ns timeout", func() Config { c := validConfig(); c.RequestTimeout = time.Nanosecond; return c }(), nil},
		{"exactly 1h timeout", func() Config { c := validConfig(); c.RequestTimeout = time.Hour; return c }(), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.cfg.Validate(); err != tc.wantErr {
				t.Errorf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://custom:9999")
	t.Setenv("OLLAMA_MODEL", "custom-model")
	t.Setenv("OLLAMA_NUM_CTX", "4096")
	t.Setenv("MAX_RETRIES", "7")
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "30")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LMSTUDIO_HOST", "http://lmstudio:1234")
	t.Setenv("LMSTUDIO_MODEL", "lm-model")
	t.Setenv("LOCAL_HOST", "http://local:8080")
	t.Setenv("LOCAL_MODEL", "local-m")
	t.Setenv("SESSION_DIR", "/tmp/lumen-test-sessions")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaHost != "http://custom:9999" {
		t.Errorf("OllamaHost = %q", cfg.OllamaHost)
	}
	if cfg.OllamaModel != "custom-model" {
		t.Errorf("OllamaModel = %q", cfg.OllamaModel)
	}
	if cfg.OllamaNumCtx != 4096 {
		t.Errorf("OllamaNumCtx = %d", cfg.OllamaNumCtx)
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d", cfg.MaxRetries)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v", cfg.RequestTimeout)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q", cfg.LogFormat)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.LMStudioHost != "http://lmstudio:1234" {
		t.Errorf("LMStudioHost = %q", cfg.LMStudioHost)
	}
	if cfg.LMStudioModel != "lm-model" {
		t.Errorf("LMStudioModel = %q", cfg.LMStudioModel)
	}
	if cfg.LocalHost != "http://local:8080" {
		t.Errorf("LocalHost = %q", cfg.LocalHost)
	}
	if cfg.LocalModel != "local-m" {
		t.Errorf("LocalModel = %q", cfg.LocalModel)
	}
	if cfg.SessionDir != "/tmp/lumen-test-sessions" {
		t.Errorf("SessionDir = %q", cfg.SessionDir)
	}
	if cfg.BackupDir != "/tmp/lumen-test-sessions/snapshots" {
		t.Errorf("BackupDir = %q", cfg.BackupDir)
	}
	if cfg.AuditLogPath != "/tmp/lumen-test-sessions/audit.jsonl" {
		t.Errorf("AuditLogPath = %q", cfg.AuditLogPath)
	}
}

func TestLoad_NumericEnvNonNumeric(t *testing.T) {
	t.Setenv("OLLAMA_NUM_CTX", "abc")
	t.Setenv("MAX_RETRIES", "xyz")
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "not-a-number")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaNumCtx != DefaultOllamaNumCtx {
		t.Errorf("OllamaNumCtx should stay default, got %d", cfg.OllamaNumCtx)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries should stay default, got %d", cfg.MaxRetries)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("RequestTimeout should stay default, got %v", cfg.RequestTimeout)
	}
}

func TestLoad_NumericEnvZeroOrNegative(t *testing.T) {
	t.Setenv("OLLAMA_NUM_CTX", "0")
	t.Setenv("MAX_RETRIES", "0")
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "0")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OllamaNumCtx != DefaultOllamaNumCtx {
		t.Errorf("OllamaNumCtx should stay default for 0, got %d", cfg.OllamaNumCtx)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries should stay default for 0, got %d", cfg.MaxRetries)
	}
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("RequestTimeout should stay default for 0, got %v", cfg.RequestTimeout)
	}
}

func TestWarnOnLooseDotEnvPerms_NonexistentFile(t *testing.T) {
	t.Parallel()
	warnOnLooseDotEnvPerms("/nonexistent/.env", slog.Default())
}

func TestWarnOnLooseDotEnvPerms_NilLogger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	os.WriteFile(p, []byte("K=V"), 0o666)
	warnOnLooseDotEnvPerms(p, nil)
}

func TestWarnOnLooseDotEnvPerms_LoosePerms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	os.WriteFile(p, []byte("K=V"), 0o666)
	// Should not panic; just log
	warnOnLooseDotEnvPerms(p, slog.Default())
}

func TestWarnOnLooseDotEnvPerms_StrictPerms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	os.WriteFile(p, []byte("K=V"), 0o600)
	warnOnLooseDotEnvPerms(p, slog.Default())
}

func TestFindDotEnv_FallbackToDotEnv(t *testing.T) {
	t.Parallel()
	result := findDotEnv()
	if result == "" {
		t.Error("findDotEnv should return non-empty string")
	}
}

func TestLoad_DefaultPaths(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemPrompt == "" {
		t.Error("SystemPrompt should not be empty")
	}
	if cfg.RequestTimeout <= 0 {
		t.Error("RequestTimeout should be positive")
	}
	if cfg.MaxRetries < 1 {
		t.Error("MaxRetries should be >= 1")
	}
}

func validConfig() Config {
	return Config{
		OllamaHost:     "http://localhost:11434",
		OllamaModel:    "test-model",
		OllamaNumCtx:   8192,
		MaxRetries:     3,
		RequestTimeout: 120 * time.Second,
		LogFormat:      "text",
		LogLevel:       "info",
	}
}
