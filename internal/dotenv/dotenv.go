// Package dotenv provides a minimal, dependency-free .env file parser.
//
// It is intentionally split into a pure Parse function (testable without
// touching the real OS environment or filesystem) and a thin Load wrapper
// that reads a file from disk.
package dotenv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Parse reads KEY=VALUE pairs from r, one per line. Blank lines and lines
// starting with "#" are ignored. Values may be optionally wrapped in single
// or double quotes, which are stripped. Malformed lines (no "=") are
// skipped rather than treated as fatal, matching typical .env tooling.
func Parse(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue // not a KEY=VALUE line; ignore rather than fail the whole file
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		if key == "" {
			continue
		}
		out[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dotenv: scan error: %w", err)
	}
	return out, nil
}

// Load reads and parses a .env file from disk. A missing file is not an
// error — it returns an empty map, since .env is always optional.
func Load(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("dotenv: opening %s: %w", path, err)
	}
	defer f.Close()
	return Parse(f)
}

// ApplyToEnviron merges parsed values into a copy of the process
// environment representation (a map), giving priority to values that are
// already non-empty. This mirrors real `export FOO=bar` always winning
// over the .env file, while an exported-but-empty variable is treated the
// same as unset (the bug we hit in the single-file version).
func ApplyToEnviron(existing map[string]string, parsed map[string]string) map[string]string {
	merged := make(map[string]string, len(existing)+len(parsed))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range parsed {
		if cur, ok := merged[k]; !ok || cur == "" {
			merged[k] = v
		}
	}
	return merged
}
