package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotenv_PermissionDenied(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("running as root, cannot test permission denied")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.env")
	if err := os.WriteFile(path, []byte("KEY=value"), 0000); err != nil {
		t.Fatal(err)
	}
	_, err := LoadDotenv(path)
	if err == nil {
		t.Fatal("expected permission error")
	}
}

func TestParseDotenv_NoEqualsSign(t *testing.T) {
	t.Parallel()
	r := strings.NewReader("NO_EQUALS_SIGN\n")
	got, err := ParseDotenv(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseDotenv_EmptyKey(t *testing.T) {
	t.Parallel()
	r := strings.NewReader("=value\n")
	got, err := ParseDotenv(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for empty key, got %v", got)
	}
}

func TestApplyEnv_ExistingEmpty(t *testing.T) {
	t.Parallel()
	existing := map[string]string{"A": ""}
	parsed := map[string]string{"A": "from_parsed", "B": "only_parsed"}
	got := ApplyEnv(existing, parsed)
	if got["A"] != "from_parsed" {
		t.Errorf("expected 'from_parsed' for empty existing, got %q", got["A"])
	}
	if got["B"] != "only_parsed" {
		t.Errorf("expected 'only_parsed', got %q", got["B"])
	}
}

func TestApplyEnv_ExistingNonEmpty(t *testing.T) {
	t.Parallel()
	existing := map[string]string{"A": "real"}
	parsed := map[string]string{"A": "parsed", "B": "only_parsed"}
	got := ApplyEnv(existing, parsed)
	if got["A"] != "real" {
		t.Errorf("expected 'real' for non-empty existing, got %q", got["A"])
	}
	if got["B"] != "only_parsed" {
		t.Errorf("expected 'only_parsed', got %q", got["B"])
	}
}

func TestLoadDotenv_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEY=value\nOTHER=hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDotenv(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "value" {
		t.Errorf("expected 'value', got %q", got["KEY"])
	}
	if got["OTHER"] != "hello" {
		t.Errorf("expected 'hello', got %q", got["OTHER"])
	}
}

func TestLoadDotenv_IsADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := LoadDotenv(dir)
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
}
