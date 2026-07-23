// Package harvest reads source code from a file or directory, strips
// comments/blank lines, and builds the context block sent to the LLM. It
// also handles before/after snapshotting for auditability.
package harvest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var reBlankLine = regexp.MustCompile(`^\s*$`)

// commentRegexCache avoids recompiling the same per-language comment
// regexes on every file during a directory harvest.
var commentRegexCache = map[string][2]*regexp.Regexp{}

// commentRegexesFor returns (fullLineRe, trailingRe) for a given comment
// prefix (e.g. "//", "#", "--"), compiling and caching them on first use.
func commentRegexesFor(prefix string) (*regexp.Regexp, *regexp.Regexp) {
	if cached, ok := commentRegexCache[prefix]; ok {
		return cached[0], cached[1]
	}
	quoted := regexp.QuoteMeta(prefix)
	full := regexp.MustCompile(`^\s*` + quoted)
	trailing := regexp.MustCompile(`\s` + quoted + `.*$`)
	commentRegexCache[prefix] = [2]*regexp.Regexp{full, trailing}
	return full, trailing
}

// MinifyCode strips full-line and trailing line comments (using whatever
// comment token matches path's language — "//", "#", "--", etc.) plus
// blank lines from a file, preserving indentation on remaining lines. For
// an unrecognized extension, or a language with no safe single-line
// comment token, only blank lines are stripped.
func MinifyCode(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("harvest: reading %s: %w", path, err)
	}

	style, _ := commentStyleForPath(path)
	var reFullLineComment, reTrailingComment *regexp.Regexp
	if style.Prefix != "" {
		reFullLineComment, reTrailingComment = commentRegexesFor(style.Prefix)
	}

	var out strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if reFullLineComment != nil && reFullLineComment.MatchString(line) {
			continue
		}
		if reTrailingComment != nil {
			line = reTrailingComment.ReplaceAllString(line, "")
		}
		if reBlankLine.MatchString(line) {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String(), nil
}

// ValidateTargetPath checks that a user-supplied path is safe to operate
// on: it must exist, and must not be a symlink pointing somewhere
// surprising (we resolve it and just confirm the resolved target exists
// and is a regular file or directory — not a device, socket, etc).
func ValidateTargetPath(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("harvest: path %q could not be resolved: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("harvest: path %q not accessible: %w", path, err)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("harvest: path %q is neither a regular file nor a directory", path)
	}
	return nil
}

// Context builds the initial "here is the code" block for either a single
// file or a directory of recognized source files across many languages
// (see languages.go), excluding test files, *-bin files, and common
// vendor/build/VCS directories.
func Context(targetPath string) (string, error) {
	if targetPath == "--chat" {
		return "Assistant: Standalone chat session initialized.\n", nil
	}

	if err := ValidateTargetPath(targetPath); err != nil {
		return "", err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return "", fmt.Errorf("harvest: %w", err)
	}

	var b strings.Builder

	if !info.IsDir() {
		b.WriteString(fmt.Sprintf("--- TARGET FILE IDENTIFIER: %s ---\n", targetPath))
		content, err := MinifyCode(targetPath)
		if err != nil {
			return "", err
		}
		b.WriteString(content)
		b.WriteString("\n\n")
		return b.String(), nil
	}

	var files []string
	err = filepath.Walk(targetPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if fi.Name() != "." && skipDirNames[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := fi.Name()
		if _, recognized := commentStyleForPath(name); !recognized {
			return nil
		}
		if strings.HasSuffix(name, "-bin") || isTestFile(path) {
			return nil
		}
		if !looksLikeText(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("harvest: walking %s: %w", targetPath, err)
	}
	sort.Strings(files)

	for _, f := range files {
		b.WriteString(fmt.Sprintf("--- SOURCE FILE ELEMENT: %s ---\n", f))
		content, err := MinifyCode(f)
		if err != nil {
			return "", err
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}

	return b.String(), nil
}

func looksLikeText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return true
	}
	for _, bt := range buf[:n] {
		if bt == 0 {
			return false
		}
	}
	return true
}

// CreateSnapshot copies targetPath into backupDir under a timestamped,
// labeled directory ("BEFORE"/"AFTER"). A missing target is silently
// skipped (e.g. --chat mode has nothing to snapshot).
func CreateSnapshot(backupDir, targetPath, label string) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		return nil
	}
	stamp := time.Now().Format("20060102_150405")
	dest := filepath.Join(backupDir, fmt.Sprintf("snap_%s_%s", label, stamp))
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("harvest: creating snapshot dir: %w", err)
	}
	if info.IsDir() {
		return copyDir(targetPath, filepath.Join(dest, filepath.Base(targetPath)))
	}
	return copyFile(targetPath, filepath.Join(dest, filepath.Base(targetPath)))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFile(path, target)
	})
}
