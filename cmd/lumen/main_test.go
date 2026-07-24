package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run --version: %v\noutput: %s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		t.Error("expected non-empty version output")
	}
	t.Logf("version output: %q", s)
}

func TestHelp(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	_ = err
	s := string(out)
	if !strings.Contains(s, "Usage") {
		t.Errorf("expected 'Usage' in help output, got: %s", s)
	}
	if !strings.Contains(s, "--auto") {
		t.Errorf("expected '--auto' in help output")
	}
}

func TestNoArgsPrintsUsage(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\noutput: %s", err, out)
	}
	if len(out) == 0 {
		t.Error("expected some output from --version")
	}
}

func TestBuildBinary(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "build", "-o", t.TempDir()+"/lumen", ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
}
