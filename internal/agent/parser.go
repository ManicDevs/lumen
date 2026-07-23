package agent

import (
	"regexp"
	"strings"
)

type fileBlock struct {
	filename string
	content  string
}

var (
	doneRe = regexp.MustCompile(`(?i)AUTO_DONE`)
	fence  = "```"
)

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

func joinStr(elems []string, sep string) string {
	return strings.Join(elems, sep)
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}

func parseFileBlocks(reply string) []fileBlock {
	var blocks []fileBlock
	lines := strings.Split(reply, "\n")
	inBlock := false
	var currentFile string
	var currentContent []string

	for _, line := range lines {
		trimmed := trimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, fence+"file:") {
				inBlock = true
				currentFile = strings.TrimPrefix(trimmed, fence+"file:")
				currentContent = nil
			} else if strings.HasPrefix(trimmed, fence+"go") {
				inBlock = true
				currentFile = "main.go"
				currentContent = nil
			}
		} else {
			if trimmed == fence {
				contentStr := joinStr(currentContent, "\n") + "\n"
				blocks = append(blocks, fileBlock{filename: currentFile, content: contentStr})
				inBlock = false
			} else {
				currentContent = append(currentContent, line)
			}
		}
	}
	return blocks
}

func parseRunBlocks(reply string) []string {
	var blocks []string
	lines := strings.Split(reply, "\n")
	inBlock := false
	var currentContent []string

	for _, line := range lines {
		trimmed := trimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, fence+"run") ||
				strings.HasPrefix(trimmed, fence+"sh") ||
				strings.HasPrefix(trimmed, fence+"bash") {
				inBlock = true
				currentContent = nil
			}
		} else {
			if trimmed == fence {
				contentStr := joinStr(currentContent, "\n") + "\n"
				blocks = append(blocks, contentStr)
				inBlock = false
			} else {
				currentContent = append(currentContent, line)
			}
		}
	}
	return blocks
}

func detectMalformedAttempts(reply string) []string {
	var issues []string
	fences := strings.Count(reply, fence)
	if fences%2 != 0 {
		issues = append(issues, "odd number of code fences")
	}

	fileOpens := 0
	lines := strings.Split(reply, "\n")
	for _, line := range lines {
		t := trimSpace(line)
		if strings.HasPrefix(t, fence+"file:") || strings.HasPrefix(t, fence+"go") {
			fileOpens++
		}
	}
	if fileOpens != len(parseFileBlocks(reply)) {
		issues = append(issues, "opened file block never closed")
	}

	runOpens := 0
	for _, line := range lines {
		t := trimSpace(line)
		if strings.HasPrefix(t, fence+"run") || strings.HasPrefix(t, fence+"sh") || strings.HasPrefix(t, fence+"bash") {
			runOpens++
		}
	}
	if runOpens != len(parseRunBlocks(reply)) {
		issues = append(issues, "opened run block never closed")
	}

	return issues
}

func SanitizeFilename(filename string) string {
	base := trimSpace(filename)
	if base == ".go" || base == "go" {
		return "main.go"
	}
	if strings.HasPrefix(base, ".") && !strings.Contains(base, "/") && len(base) <= 5 {
		return "main" + base
	}
	return base
}
