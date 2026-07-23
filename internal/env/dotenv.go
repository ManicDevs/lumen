// Package env provides a minimal .env file parser that treats environment
// variables set in the real process environment as authoritative — .env file
// values are only used as lower-priority fallbacks.
package env

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseDotenv reads key=value pairs from a reader, ignoring blank lines and
// #-prefixed comments. Values are stripped of surrounding quotes.
func ParseDotenv(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
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

// LoadDotenv opens a .env file (returning an empty map if it does not
// exist) and parses it with ParseDotenv.
func LoadDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("dotenv: opening %s: %w", path, err)
	}
	defer f.Close()
	return ParseDotenv(f)
}

// ApplyEnv merges two maps, giving priority to existing (real process env)
// keys. A key in existing with an empty-string value is treated as unset and
// falls through to the parsed value.
func ApplyEnv(existing map[string]string, parsed map[string]string) map[string]string {
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
