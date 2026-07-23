package harvest

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDirectoryWalk(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	for _, file := range []string{"a.go", "b.py", "c.js", "d.txt", "e.md"} {
		path := filepath.Join(tempDir, file)
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	subdir := filepath.Join(tempDir, "sub")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	for _, file := range []string{"x.go", "y.py"} {
		path := filepath.Join(subdir, file)
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("failed to create subdir file: %v", err)
		}
	}

	testFiles := make(map[string]bool)
	callback := func(path string, info fs.FileInfo, err error) error {
		if info == nil || info.IsDir() {
			return nil
		}
		testFiles[info.Name()] = true
		return nil
	}

	if err := directoryWalk(tempDir, callback); err != nil {
		t.Fatalf("directoryWalk failed: %v", err)
	}

	expectedFiles := []string{"a.go", "b.py", "c.js", "d.txt", "e.md", "x.go", "y.py"}
	for _, name := range expectedFiles {
		if !testFiles[name] {
			t.Errorf("expected file %s not found in walk results", name)
		}
	}

	if len(testFiles) != len(expectedFiles) {
		t.Errorf("expected %d files, got %d", len(expectedFiles), len(testFiles))
	}

	dirWithSubDir := filepath.Join(tempDir, "nested", "deep")
	if err := os.MkdirAll(dirWithSubDir, 2755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	if err := directoryWalk(tempDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "deep" {
			t.Errorf("unexpected entry %s during walk", info.Name())
		}
		return nil
	}); err != nil {
		t.Fatalf("walk with callback failed: %v", err)
	}

	if err := directoryWalk("/nonexistent", func(string, fs.FileInfo, error) error { return nil }); err == nil {
		t.Error("directoryWalk expected error for nonexistent path")
	}
}