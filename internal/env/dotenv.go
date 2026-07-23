package env

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

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
