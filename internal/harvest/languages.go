package harvest

import (
	"regexp"
	"strings"
)

// commentStyle describes how to strip single-line comments for a given
// source language. Prefix is the token that starts a line comment (e.g.
// "//", "#", "--"). Languages with no well-defined single-line comment
// token (or where stripping would be unsafe, e.g. because the token
// collides with common syntax) use an empty Prefix, meaning only blank
// lines get stripped.
type commentStyle struct {
	Prefix string
}

// langExtensions maps a lowercased file extension (including the leading
// dot) to its comment style. This is the single source of truth for
// "what counts as source code" during a directory harvest.
var langExtensions = map[string]commentStyle{
	// C-style "//" line comments
	".go":     {"//"},
	".c":      {"//"},
	".h":      {"//"},
	".cc":     {"//"},
	".cpp":    {"//"},
	".cxx":    {"//"},
	".hpp":    {"//"},
	".hh":     {"//"},
	".java":   {"//"},
	".js":     {"//"},
	".jsx":    {"//"},
	".ts":     {"//"},
	".tsx":    {"//"},
	".mjs":    {"//"},
	".cjs":    {"//"},
	".cs":     {"//"},
	".rs":     {"//"},
	".swift":  {"//"},
	".kt":     {"//"},
	".kts":    {"//"},
	".scala":  {"//"},
	".php":    {"//"},
	".dart":   {"//"},
	".m":      {"//"}, // Objective-C (best-effort; MATLAB uses "%")
	".mm":     {"//"},
	".groovy": {"//"},

	// "#" line comments
	".py":   {"#"},
	".rb":   {"#"},
	".sh":   {"#"},
	".bash": {"#"},
	".zsh":  {"#"},
	".pl":   {"#"},
	".pm":   {"#"},
	".yaml": {"#"},
	".yml":  {"#"},
	".toml": {"#"},
	".r":    {"#"},
	".ex":   {"#"},
	".exs":  {"#"},

	// "--" line comments
	".sql": {"--"},
	".lua": {"--"},
	".hs":  {"--"},

	// No safe single-line comment stripping (blank-line stripping only)
	".css":  {""},
	".scss": {""},
	".html": {""},
	".xml":  {""},
	".json": {""},
}

// testFilePatterns match filenames that should be excluded from a
// directory harvest because they're test files rather than source under
// review, across common per-language conventions.
var testFilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`_test\.[a-zA-Z0-9]+$`),               // Go, generic: foo_test.go
	regexp.MustCompile(`\.test\.[a-zA-Z0-9]+$`),              // JS/TS: foo.test.js
	regexp.MustCompile(`\.spec\.[a-zA-Z0-9]+$`),              // JS/TS: foo.spec.ts
	regexp.MustCompile(`(^|/)test_[a-zA-Z0-9_]+\.py$`),       // Python: test_foo.py
	regexp.MustCompile(`(^|/)Test[A-Z][a-zA-Z0-9_]*\.java$`), // Java: TestFoo.java
	regexp.MustCompile(`_spec\.rb$`),                         // Ruby: foo_spec.rb
}

// skipDirNames are directories never walked into during a directory
// harvest — build artifacts, dependency trees, and VCS metadata add
// noise (and in the case of node_modules/vendor, can be enormous).
var skipDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".idea":        true,
	".vscode":      true,
}

// commentStyleForPath returns the comment style for path's extension and
// whether that extension is a recognized source language at all.
func commentStyleForPath(path string) (commentStyle, bool) {
	ext := strings.ToLower(extOf(path))
	style, ok := langExtensions[ext]
	return style, ok
}

func extOf(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return ""
	}
	return path[i:]
}

// isTestFile reports whether path matches a known test-file naming
// convention, using forward-slash-normalized path for the patterns that
// anchor on a directory separator.
func isTestFile(path string) bool {
	normalized := strings.ReplaceAll(path, "\\", "/")
	for _, re := range testFilePatterns {
		if re.MatchString(normalized) {
			return true
		}
	}
	return false
}
