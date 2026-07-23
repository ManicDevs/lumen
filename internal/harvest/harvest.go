package harvest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

// MaxFileSize is the maximum file size (in bytes) we are willing to read
// into memory during MinifyCode. Files larger than this return an error.
const MaxFileSize = 16 * 1024 * 1024 // 16 MiB

// MinifyCode strips full-line and trailing line comments (using whatever
// comment token matches path's language — "//", "#", "--", etc.) plus
// blank lines from a file, preserving indentation on remaining lines. For
// an unrecognized extension, or a language with no safe single-line
// comment token, only blank lines are stripped.
func MinifyCode(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("harvest: stat %s: %w", path, err)
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("harvest: %s is %d bytes (max %d)", path, info.Size(), MaxFileSize)
	}
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
