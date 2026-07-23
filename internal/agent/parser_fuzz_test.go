package agent

import (
	"strings"
	"testing"
)

func FuzzParseFileBlocks(f *testing.F) {
	seeds := []string{
		"```file:main.go\npackage main\n```",
		"```go\nfunc main() {}\n```",
		"```file:test.go\ncontent\n```\n```run\necho hi\n```",
		"no blocks here",
		"",
		"```file:\nno filename\n```",
		"``````",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		blocks := parseFileBlocks(input)
		for _, b := range blocks {
			if strings.TrimSpace(b.filename) == "" {
				t.Errorf("empty filename in block: %+v", b)
			}
			if b.filename != SanitizeFilename(b.filename) {
				t.Errorf("filename not properly sanitized: %q", b.filename)
			}
		}
	})
}

func FuzzParseRunBlocks(f *testing.F) {
	seeds := []string{
		"```run\necho hello\n```",
		"```sh\nls -la\n```",
		"```bash\n./script.sh\n```",
		"no run blocks",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		blocks := parseRunBlocks(input)
		for _, b := range blocks {
			if strings.TrimSpace(b) == "" {
				t.Errorf("empty command block")
			}
			if strings.Contains(b, "```") {
				t.Errorf("block contains unprocessed fence: %q", b)
			}
		}
	})
}

func FuzzSanitizeFilename(f *testing.F) {
	seeds := []string{
		"main.go",
		".go",
		"go",
		".py",
		"path/to/file.go",
		"",
		".",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		result := SanitizeFilename(input)
		if result == "" && input != "" {
			t.Errorf("empty result for non-empty input %q", input)
		}
		if strings.Contains(result, "\n") {
			t.Errorf("newline in sanitized filename: %q", result)
		}
		if strings.Contains(result, "\r") {
			t.Errorf("carriage return in sanitized filename: %q", result)
		}
	})
}
