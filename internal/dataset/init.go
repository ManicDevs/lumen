package dataset

import (
	"fmt"
	"os"
	"path/filepath"
)

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func RunInit() error {
	commitsDir := filepath.Join(DatasetRoot, "commits")
	stageDir := filepath.Join(DatasetRoot, "stage")
	refsHeadsDir := filepath.Join(DatasetRoot, "refs", "heads")

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

	keepPath := filepath.Join(stageDir, ".gitkeep")
	if _, err := os.Stat(keepPath); os.IsNotExist(err) {
		_ = os.WriteFile(keepPath, nil, 0644)
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
	fmt.Println("(no commits yet — run --easter-egg --pipe-dataset to record the first one)")

	return nil
}
