package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := InitIfNeeded(context.Background(), dir); err != nil {
		t.Fatalf("InitIfNeeded: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	return dir
}

func TestInitIfNeeded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := InitIfNeeded(context.Background(), dir); err != nil {
		t.Fatalf("InitIfNeeded: %v", err)
	}
	if !IsRepo(context.Background(), dir) {
		t.Fatal("expected repo after init")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); os.IsNotExist(err) {
		t.Fatal("expected .gitignore to be created")
	}
}

func TestInitIfNeededAlreadyRepo(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t)
	if err := InitIfNeeded(context.Background(), dir); err != nil {
		t.Fatalf("InitIfNeeded on existing repo: %v", err)
	}
}

func TestIsRepo(t *testing.T) {
	t.Parallel()
	if IsRepo(context.Background(), t.TempDir()) {
		t.Fatal("expected false for non-repo dir")
	}
	dir := setupTestRepo(t)
	if !IsRepo(context.Background(), dir) {
		t.Fatal("expected true for repo dir")
	}
}

func TestCommitDataset(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t)
	sha, err := CommitDataset(context.Background(), dir, "test commit")
	if err != nil {
		t.Fatalf("CommitDataset: %v", err)
	}
	if sha == "" {
		t.Fatal("expected non-empty SHA")
	}
}

func TestCommitDatasetEmpty(t *testing.T) {
	t.Parallel()
	_, err := CommitDataset(context.Background(), "", "msg")
	if err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestCommitDatasetNoChanges(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t)
	CommitDataset(context.Background(), dir, "initial")
	sha, err := CommitDataset(context.Background(), dir, "no changes")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if sha != "" {
		t.Fatalf("expected empty SHA, got: %s", sha)
	}
}

func TestGetStatus(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t)
	status, err := GetStatus(context.Background(), dir)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Untracked != 2 {
		t.Errorf("expected 2 untracked (test.txt + .gitignore), got %d", status.Untracked)
	}
}

func TestGetStatusEmpty(t *testing.T) {
	t.Parallel()
	_, err := GetStatus(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestCreateTag(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t)
	CommitDataset(context.Background(), dir, "initial commit")
	if err := CreateTag(context.Background(), dir, "v0.1.0", "release"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
}

func TestCreateTagEmptyRoot(t *testing.T) {
	t.Parallel()
	if err := CreateTag(context.Background(), "", "v1", "m"); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestCreateTagEmptyName(t *testing.T) {
	t.Parallel()
	dir := setupTestRepo(t)
	if err := CreateTag(context.Background(), dir, "", "m"); err == nil {
		t.Fatal("expected error for empty tag name")
	}
}
