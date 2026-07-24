package harvest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeText_NullBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.bin")
	data := []byte("hello\x00world")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if looksLikeText(path) {
		t.Error("expected false for file with null bytes")
	}
}

func TestLooksLikeText_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if !looksLikeText(path) {
		t.Error("expected true for empty file")
	}
}

func TestLooksLikeText_Nonexistent(t *testing.T) {
	t.Parallel()
	if looksLikeText("/nonexistent/file.txt") {
		t.Error("expected false for nonexistent file")
	}
}

func TestValidateTargetPath_Symlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := ValidateTargetPath(link)
	if err != nil {
		t.Errorf("expected nil error for valid symlink, got: %v", err)
	}
}

func TestValidateTargetPath_DeviceFile(t *testing.T) {
	t.Parallel()
	err := ValidateTargetPath("/dev/null")
	if err == nil {
		t.Error("expected error for device file")
	}
}

func TestContext_NonExistentPath(t *testing.T) {
	t.Parallel()
	_, err := Context("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestMinifyCode_MaxFileSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.go")
	data := make([]byte, MaxFileSize+1)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := MinifyCode(path)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestMinifyCode_NonexistentFile(t *testing.T) {
	t.Parallel()
	_, err := MinifyCode("/nonexistent/file.go")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
