package harvest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	mu sync.Mutex
)

func commentRegexesFor(prefix string) (*regexp.Regexp, *regexp.Regexp) {
	if cached, ok := commentRegexCache[prefix]; ok {
		return cached[0], cached[1]
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	if cached, ok := commentRegexCache[prefix]; ok {
		return cached[0], cached[1]
	}
	
	quoted := regexp.QuoteMeta(prefix)
	full := regexp.MustCompile(`^\s*` + quoted)
	trailing := regexp.MustCompile(`\s` + quoted + `.*$`)
	commentRegexCache[prefix] = [2]*regexp.Regexp{full, trailing}
	return full, trailing
}

const MaxFileSize = 16 * 1024 * 1024

func MinifyCode(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > MaxFileSize {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
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

func ValidateTargetPath(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return err
	}
	return nil
}

func Context(targetPath string) (string, error) {
	if err := ValidateTargetPath(targetPath); err != nil {
		return "", err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("walking %s: %w", targetPath, err)
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