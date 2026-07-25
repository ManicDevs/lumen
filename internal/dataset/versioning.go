package dataset

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DatasetVersion represents a versioned snapshot of the dataset.
type DatasetVersion struct {
	ID         string    `json:"id"`
	CommitSHA  string    `json:"commit_sha"`
	Message    string    `json:"message"`
	Datapoints int       `json:"datapoints"`
	CreatedAt  time.Time `json:"created_at"`
	Tags       []string  `json:"tags,omitempty"`
}

// VersionDiff describes the difference between two versions.
type VersionDiff struct {
	From          string   `json:"from"`
	To            string   `json:"to"`
	AddedFiles    []string `json:"added"`
	RemovedFiles  []string `json:"removed"`
	ModifiedFiles []string `json:"modified"`
}

// ListVersions returns all dataset versions (git tags matching v* pattern).
func ListVersions(root string) ([]DatasetVersion, error) {
	if root == "" {
		return nil, fmt.Errorf("dataset versioning: root is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all tags matching v*.
	cmd := exec.CommandContext(ctx, "git", "tag", "-l", "v*", "--sort=-version:refname")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git tag list: %w", err)
	}
	tagLines := strings.Split(strings.TrimSpace(string(out)), "\n")

	var versions []DatasetVersion
	for _, tag := range tagLines {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}

		// Get commit SHA for this tag.
		shaCmd := exec.CommandContext(ctx, "git", "rev-list", "-n", "1", tag)
		shaCmd.Dir = root
		shaOut, err := shaCmd.Output()
		if err != nil {
			continue
		}
		sha := strings.TrimSpace(string(shaOut))

		// Get tagger date.
		dateCmd := exec.CommandContext(ctx, "git", "log", "-1", "--format=%aI", tag)
		dateCmd.Dir = root
		dateOut, err := dateCmd.Output()
		createdAt := time.Now()
		if err == nil {
			if t, tErr := time.Parse(time.RFC3339, strings.TrimSpace(string(dateOut))); tErr == nil {
				createdAt = t
			}
		}

		// Get tag message.
		msgCmd := exec.CommandContext(ctx, "git", "tag", "-l", "--format=%(contents)", tag)
		msgCmd.Dir = root
		msgOut, err := msgCmd.Output()
		message := ""
		if err == nil {
			message = strings.TrimSpace(string(msgOut))
		}

		// Count datapoints at this tag.
		dpCmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "--name-only", tag)
		dpCmd.Dir = root
		dpOut, err := dpCmd.Output()
		datapoints := 0
		if err == nil {
			files := strings.Split(strings.TrimSpace(string(dpOut)), "\n")
			for _, f := range files {
				if strings.HasSuffix(f, ".json") && strings.Contains(f, "commit_") {
					datapoints++
				}
			}
		}

		versions = append(versions, DatasetVersion{
			ID:         tag,
			CommitSHA:  sha,
			Message:    message,
			Datapoints: datapoints,
			CreatedAt:  createdAt,
			Tags:       []string{tag},
		})
	}

	return versions, nil
}

// CreateVersion creates a git tag for the current dataset state.
func CreateVersion(root, tag, message string) (*DatasetVersion, error) {
	if root == "" {
		return nil, fmt.Errorf("dataset versioning: root is empty")
	}
	if tag == "" {
		return nil, fmt.Errorf("dataset versioning: tag is empty")
	}
	if message == "" {
		message = fmt.Sprintf("dataset version %s", tag)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stage all changes.
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = root
	if out, err := addCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// Commit if there are staged changes.
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")
	diffCmd.Dir = root
	if diffCmd.Run() != nil {
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
		commitCmd.Dir = root
		if out, err := commitCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	// Create annotated tag.
	tagCmd := exec.CommandContext(ctx, "git", "tag", "-a", tag, "-m", message)
	tagCmd.Dir = root
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git tag: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// Get SHA of the tagged commit.
	shaCmd := exec.CommandContext(ctx, "git", "rev-list", "-n", "1", tag)
	shaCmd.Dir = root
	shaOut, err := shaCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-list: %w", err)
	}
	sha := strings.TrimSpace(string(shaOut))

	return &DatasetVersion{
		ID:        tag,
		CommitSHA: sha,
		Message:   message,
		CreatedAt: time.Now(),
		Tags:      []string{tag},
	}, nil
}

// RollbackTo reverts the dataset to a specific version by checking out the tagged commit.
func RollbackTo(root, versionID string) error {
	if root == "" {
		return fmt.Errorf("dataset versioning: root is empty")
	}
	if versionID == "" {
		return fmt.Errorf("dataset versioning: version id is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stash any uncommitted changes.
	stashCmd := exec.CommandContext(ctx, "git", "stash", "push", "-m", "auto-stash before rollback")
	stashCmd.Dir = root
	stashCmd.Run() // ignore errors — stash may be empty

	// Checkout the tag.
	checkoutCmd := exec.CommandContext(ctx, "git", "checkout", versionID)
	checkoutCmd.Dir = root
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// DiffVersions shows what changed between two dataset versions.
func DiffVersions(root, from, to string) (*VersionDiff, error) {
	if root == "" {
		return nil, fmt.Errorf("dataset versioning: root is empty")
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("dataset versioning: from and to are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	diff := &VersionDiff{From: from, To: to}

	// Get added/modified files.
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--name-status", from, to)
	diffCmd.Dir = root
	out, err := diffCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		file := parts[1]
		switch status {
		case "A":
			diff.AddedFiles = append(diff.AddedFiles, file)
		case "D":
			diff.RemovedFiles = append(diff.RemovedFiles, file)
		case "M":
			diff.ModifiedFiles = append(diff.ModifiedFiles, file)
		}
	}

	return diff, nil
}
