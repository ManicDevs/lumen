package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

// RunDatasetInit is the "git init" equivalent for the versioned synthetic
// dataset under data/datasets. Previously that layout only ever came into
// existence as a side effect of the first `--easter-egg --pipe-dataset`
// run (see RunBunnyEasterEgg), which meant there was no way to stand up
// an empty, ready-to-commit-into dataset repo ahead of time, and no clear
// signal for whether one already existed.
//
// Mirrors real git's init semantics on purpose:
//   - commits/, stage/, and refs/heads/ are created up front.
//   - refs/heads/master itself is NOT written here — same as a fresh git
//     repo has no refs/heads/master until the first commit lands. writeCommit
//     (easter_egg.go) already handles "ref file missing" as "this is the
//     first commit" via its os.ReadFile err check, so leaving it absent is
//     both correct and required for that logic to behave.
//   - Re-running against an already-initialized dataset repo is a no-op
//     that reports "Reinitialized", not an error — same as `git init` run
//     twice in the same directory.
func RunDatasetInit() error {
	commitsDir := filepath.Join(datasetRoot, "commits")
	stageDir := filepath.Join(datasetRoot, "stage")
	refsHeadsDir := filepath.Join(datasetRoot, "refs", "heads")

	alreadyExists := dirExists(commitsDir) && dirExists(refsHeadsDir)

	if err := os.MkdirAll(commitsDir, 0755); err != nil {
		return fmt.Errorf("dataset init: creating commits dir: %w", err)
	}
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return fmt.Errorf("dataset init: creating stage dir: %w", err)
	}
	if err := os.MkdirAll(refsHeadsDir, 0755); err != nil {
		return fmt.Errorf("dataset init: creating refs dir: %w", err)
	}

	// stage/ is gitignored (data/datasets/stage/* in .gitignore, since its
	// contents are transient in-progress runs) except for a .gitkeep, so a
	// fresh checkout of the real repo still has the directory. Write it
	// here too so `--dataset-init` alone is enough to leave the tree in a
	// state that survives being committed to the real repo empty.
	keepPath := filepath.Join(stageDir, ".gitkeep")
	if _, err := os.Stat(keepPath); os.IsNotExist(err) {
		_ = os.WriteFile(keepPath, nil, 0644)
	}

	absPath, err := filepath.Abs(datasetRoot)
	if err != nil {
		absPath = datasetRoot
	}

	if alreadyExists {
		fmt.Printf("Reinitialized existing dataset repository in %s\n", absPath)
	} else {
		fmt.Printf("Initialized empty dataset repository in %s\n", absPath)
	}
	fmt.Println("(no commits yet — run --easter-egg --pipe-dataset to record the first one)")

	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
