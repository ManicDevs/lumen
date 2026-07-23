package output

import (
	"strings"
	"testing"
)

func FuzzSecrets(f *testing.F) {
	seeds := []string{
		"hello world",
		"api_key=secret123",
		"token=abc def ghi",
		"",
		"a",
	}
	for _, s := range seeds {
		f.Add(s, "secret123")
	}

	f.Fuzz(func(t *testing.T, input, secret string) {
		result := Secrets(input, secret)
		if len(input) > 0 && secret != "" && strings.Contains(input, secret) {
			if strings.Contains(result, secret) {
				t.Errorf("secret %q still present after redaction", secret)
			}
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("expected [REDACTED] in output for input containing secret")
			}
		}
	})
}

func FuzzURLQueryParam(f *testing.F) {
	seeds := []string{
		"https://example.com/path?key=value&other=1",
		"http://localhost:8080/api?token=abc",
		"no-query-string",
		"",
	}
	for _, s := range seeds {
		f.Add(s, "key")
	}

	f.Fuzz(func(t *testing.T, rawURL, param string) {
		result := URLQueryParam(rawURL, param)
		if param == "" {
			if result != rawURL {
				t.Errorf("empty param should return input unchanged, got %q", result)
			}
			return
		}
		if strings.Contains(rawURL, param+"=") {
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("expected [REDACTED] for param %q in %q, got %q", param, rawURL, result)
			}
		}
	})
}
