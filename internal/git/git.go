package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CommitDataset stages all files under the dataset root and creates a
// commit with the given message. Returns the new commit SHA or an error.
func CommitDataset(ctx context.Context, root, message string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("git: root is empty")
	}
	if message == "" {
		message = fmt.Sprintf("dataset: auto-commit at %s", time.Now().Format(time.RFC3339))
	}
	message = strings.ReplaceAll(message, "\n", " ")

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "add", "-A", root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(out)))
	}

	cmd = exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")
	cmd.Dir = root
	if cmd.Run() == nil {
		return "", nil
	}

	cmd = exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}

	shaCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	shaCmd.Dir = root
	shaOut, err := shaCmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(shaOut)), nil
}

// CreateTag creates an annotated git tag at the current HEAD.
func CreateTag(ctx context.Context, root, name, message string) error {
	if root == "" {
		return fmt.Errorf("git: root is empty")
	}
	if name == "" {
		return fmt.Errorf("git: tag name is empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := []string{"tag", "-a", name, "-m", message}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git tag: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Status returns the number of staged, modified, and untracked files.
type Status struct {
	Staged    int
	Modified  int
	Untracked int
}

func GetStatus(ctx context.Context, root string) (*Status, error) {
	if root == "" {
		return nil, fmt.Errorf("git: root is empty")
	}
	s := &Status{}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--name-only")
	cmd.Dir = root
	out, err := cmd.Output()
	if err == nil {
		s.Staged = len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		if strings.TrimSpace(string(out)) == "" {
			s.Staged = 0
		}
	}

	cmd = exec.CommandContext(ctx, "git", "diff", "--name-only")
	cmd.Dir = root
	out, err = cmd.Output()
	if err == nil {
		s.Modified = len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		if strings.TrimSpace(string(out)) == "" {
			s.Modified = 0
		}
	}

	cmd = exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err = cmd.Output()
	if err == nil {
		s.Untracked = len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		if strings.TrimSpace(string(out)) == "" {
			s.Untracked = 0
		}
	}

	return s, nil
}

// IsRepo checks whether the given path is inside a git repository.
func IsRepo(ctx context.Context, root string) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = root
	return cmd.Run() == nil
}

// InitIfNeeded initialises a git repo if one does not exist at root.
func InitIfNeeded(ctx context.Context, root string) error {
	if IsRepo(ctx, root) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w: %s", err, strings.TrimSpace(string(out)))
	}
	gitignore := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		if werr := os.WriteFile(gitignore, []byte("*.log\n*.tmp\n"), 0o644); werr != nil {
			return fmt.Errorf("write .gitignore: %w", werr)
		}
	}
	return nil
}
