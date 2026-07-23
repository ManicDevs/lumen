package harvest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMinifyCode_StripsCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	src := "package foo\n\n// full line comment\nfunc bar() { // trailing\n\treturn // also trailing\n}\n\n\n"
	path := writeFile(t, dir, "foo.go", src)

	got, err := MinifyCode(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "full line comment") {
		t.Errorf("full-line comment not stripped: %q", got)
	}
	if strings.Contains(got, "trailing") {
		t.Errorf("trailing comment not stripped: %q", got)
	}
	if strings.Contains(got, "package foo") == false {
		t.Errorf("expected code line to survive: %q", got)
	}
}

func TestContext_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	got, err := Context(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "TARGET FILE IDENTIFIER") {
		t.Errorf("expected file identifier header, got: %q", got)
	}
	if !strings.Contains(got, "package main") {
		t.Errorf("expected source content, got: %q", got)
	}
}

func TestContext_DirectoryExcludesTestAndBinFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "a_test.go", "package a\n// should be excluded\n")
	writeFile(t, dir, "weird-bin", "package a\n// should be excluded too\n")
	writeFile(t, dir, "readme.md", "not go, should be excluded\n")

	got, err := Context(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "a_test.go") {
		t.Errorf("_test.go file should be excluded: %q", got)
	}
	if strings.Contains(got, "weird-bin") {
		t.Errorf("-bin file should be excluded: %q", got)
	}
	if strings.Contains(got, "readme.md") {
		t.Errorf("non-.go file should be excluded: %q", got)
	}
	if !strings.Contains(got, "a.go") {
		t.Errorf("expected a.go to be included: %q", got)
	}
}

func TestContext_NonexistentPathErrors(t *testing.T) {
	_, err := Context("/nonexistent/path/should/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestValidateTargetPath_RejectsMissing(t *testing.T) {
	if err := ValidateTargetPath("/nonexistent/path"); err == nil {
		t.Error("expected error for missing path")
	}
}

func TestValidateTargetPath_AcceptsRealFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "ok.go", "package ok\n")
	if err := ValidateTargetPath(path); err != nil {
		t.Errorf("unexpected error for valid file: %v", err)
	}
}

func TestMinifyCode_PythonHashComments(t *testing.T) {
	dir := t.TempDir()
	src := "def foo():\n    # full line comment\n    return 1  # trailing\n\n\n"
	path := writeFile(t, dir, "foo.py", src)

	got, err := MinifyCode(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "full line comment") {
		t.Errorf("full-line comment not stripped: %q", got)
	}
	if strings.Contains(got, "trailing") {
		t.Errorf("trailing comment not stripped: %q", got)
	}
	if !strings.Contains(got, "def foo():") {
		t.Errorf("expected code line to survive: %q", got)
	}
}

func TestMinifyCode_SQLDashDashComments(t *testing.T) {
	dir := t.TempDir()
	src := "SELECT 1;\n-- a comment\nSELECT 2; -- trailing\n"
	path := writeFile(t, dir, "query.sql", src)

	got, err := MinifyCode(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "a comment") || strings.Contains(got, "trailing") {
		t.Errorf("comments not stripped: %q", got)
	}
	if !strings.Contains(got, "SELECT 1;") || !strings.Contains(got, "SELECT 2;") {
		t.Errorf("expected code lines to survive: %q", got)
	}
}

func TestContext_DirectoryIncludesMultipleLanguages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, "app.py", "def main():\n    pass\n")
	writeFile(t, dir, "index.ts", "export const x = 1;\n")
	writeFile(t, dir, "app.test.ts", "test('x', () => {});\n")
	writeFile(t, dir, "test_app.py", "should_be_excluded = 1\n")
	writeFile(t, dir, "node_modules/pkg/index.js", "should_be_excluded_dir = 1;\n")

	got, err := Context(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"main.go", "app.py", "index.ts"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %s to be included: %q", want, got)
		}
	}
	if strings.Contains(got, "app.test.ts") {
		t.Errorf("app.test.ts should be excluded: %q", got)
	}
	if strings.Contains(got, "should_be_excluded = 1") {
		t.Errorf("test_app.py should be excluded: %q", got)
	}
	if strings.Contains(got, "should_be_excluded_dir") {
		t.Errorf("node_modules contents should be excluded: %q", got)
	}
}
