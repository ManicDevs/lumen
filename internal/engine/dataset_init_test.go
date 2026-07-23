package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempCwd chdirs into a fresh temp directory for the duration of the
// test, since RunDatasetInit (like RunBunnyEasterEgg and writeCommit)
// operates on the package-level relative datasetRoot rather than an
// injected path.
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

func TestRunDatasetInit_FreshLayout(t *testing.T) {
	withTempCwd(t)

	if err := RunDatasetInit(); err != nil {
		t.Fatalf("RunDatasetInit: %v", err)
	}

	for _, want := range []string{
		filepath.Join(datasetRoot, "commits"),
		filepath.Join(datasetRoot, "stage"),
		filepath.Join(datasetRoot, "refs", "heads"),
	} {
		if !dirExists(want) {
			t.Errorf("expected directory %s to exist after init", want)
		}
	}

	keep := filepath.Join(datasetRoot, "stage", ".gitkeep")
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("expected %s to exist after init: %v", keep, err)
	}

	// Mirrors real git: no refs/heads/master until the first commit.
	refsPath := filepath.Join(datasetRoot, "refs", "heads", "master")
	if _, err := os.Stat(refsPath); !os.IsNotExist(err) {
		t.Errorf("expected no refs/heads/master before any commit, stat err = %v", err)
	}
}

func TestRunDatasetInit_IdempotentOnExistingRepo(t *testing.T) {
	withTempCwd(t)

	if err := RunDatasetInit(); err != nil {
		t.Fatalf("first RunDatasetInit: %v", err)
	}

	// Simulate a commit having landed since the first init.
	commitsDir := filepath.Join(datasetRoot, "commits")
	refsPath := filepath.Join(datasetRoot, "refs", "heads", "master")
	if _, err := writeCommit(commitsDir, refsPath, "test-model", []Datapoint{{Prompt: "p", Response: "r"}}); err != nil {
		t.Fatalf("writeCommit: %v", err)
	}

	if err := RunDatasetInit(); err != nil {
		t.Fatalf("second RunDatasetInit: %v", err)
	}

	// Re-running init must not disturb an existing ref/commit.
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
