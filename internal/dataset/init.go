package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// RunInit creates the dataset directory structure (commits, stage, refs)
// under data/datasets/. It is idempotent — re-running after initialisation
// prints a "Reinitialized" message.
func RunInit() error {
	commitsDir := filepath.Join(DatasetRoot, "commits")
	stageDir := filepath.Join(DatasetRoot, "stage")
	refsHeadsDir := filepath.Join(DatasetRoot, "refs", "heads")

	alreadyExists := dirExists(commitsDir) && dirExists(refsHeadsDir)

	// Check if there are any actual commits
	hasCommits := false
	if dirExists(commitsDir) {
		entries, err := os.ReadDir(commitsDir)
		if err != nil {
			return fmt.Errorf("dataset init: reading commits dir: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "commit_") && strings.HasSuffix(e.Name(), ".json") {
				hasCommits = true
				break
			}
		}
	}

	if err := os.MkdirAll(commitsDir, 0755); err != nil {
		return fmt.Errorf("dataset init: creating commits dir: %w", err)
	}
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return fmt.Errorf("dataset init: creating stage dir: %w", err)
	}
	if err := os.MkdirAll(refsHeadsDir, 0755); err != nil {
		return fmt.Errorf("dataset init: creating refs dir: %w", err)
	}

	keepPath := filepath.Join(stageDir, ".gitkeep")
	if _, err := os.Stat(keepPath); os.IsNotExist(err) {
		if err := os.WriteFile(keepPath, nil, 0644); err != nil {
			return fmt.Errorf("dataset init: creating .gitkeep: %w", err)
		}
	}

	absPath, err := filepath.Abs(DatasetRoot)
	if err != nil {
		absPath = DatasetRoot
	}

	if alreadyExists {
		fmt.Printf("Reinitialized existing dataset repository in %s\n", absPath)
	} else {
		fmt.Printf("Initialized empty dataset repository in %s\n", absPath)
	}

	if hasCommits {
		// Count commits
		entries, err := os.ReadDir(commitsDir)
		if err != nil {
			return fmt.Errorf("dataset init: reading commits dir: %w", err)
		}
		count := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "commit_") && strings.HasSuffix(e.Name(), ".json") {
				count++
			}
		}
		fmt.Printf("(%d commit(s) found)\n", count)
	} else {
		fmt.Println("(no commits yet — run --easter-egg --pipe-dataset to record the first one)")
	}

	return nil
}
