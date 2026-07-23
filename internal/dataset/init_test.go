package dataset

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempCwd(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
	return dir
}

func TestRunInit_FreshLayout(t *testing.T) {
	withTempCwd(t)

	if err := RunInit(); err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	for _, want := range []string{
		filepath.Join(DatasetRoot, "commits"),
		filepath.Join(DatasetRoot, "stage"),
		filepath.Join(DatasetRoot, "refs", "heads"),
	} {
		if !dirExists(want) {
			t.Errorf("expected directory %s to exist after init", want)
		}
	}

	keep := filepath.Join(DatasetRoot, "stage", ".gitkeep")
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("expected %s to exist after init: %v", keep, err)
	}

	refsPath := filepath.Join(DatasetRoot, "refs", "heads", "master")
	if _, err := os.Stat(refsPath); !os.IsNotExist(err) {
		t.Errorf("expected no refs/heads/master before any commit, stat err = %v", err)
	}
}

func TestRunInit_IdempotentOnExistingRepo(t *testing.T) {
	withTempCwd(t)

	if err := RunInit(); err != nil {
		t.Fatalf("first RunInit: %v", err)
	}

	commitsDir := filepath.Join(DatasetRoot, "commits")
	refsPath := filepath.Join(DatasetRoot, "refs", "heads", "master")
	if _, err := writeCommit(commitsDir, refsPath, "test-model", []Datapoint{{Prompt: "p", Response: "r"}}); err != nil {
		t.Fatalf("writeCommit: %v", err)
	}

	if err := RunInit(); err != nil {
		t.Fatalf("second RunInit: %v", err)
	}

	if _, err := os.Stat(refsPath); err != nil {
		t.Errorf("expected refs/heads/master to survive a re-init: %v", err)
	}
	entries, err := os.ReadDir(commitsDir)
	if err != nil {
		t.Fatalf("reading commits dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly 1 commit to survive re-init, got %d", len(entries))
	}
}
