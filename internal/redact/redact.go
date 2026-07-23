// Package redact provides helpers to strip secrets out of strings before
// they're logged, printed, or included in error messages. This exists
// because the cloud API key travels in a URL query parameter
// (?key=...), and it's easy to accidentally log or error-wrap that full
// URL without thinking about it.
package redact

import "strings"

const placeholder = "[REDACTED]"

// Secrets scrubs every occurrence of each non-empty secret in s, replacing
// it with a placeholder. Safe to call with an empty secret list.
func Secrets(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		s = strings.ReplaceAll(s, secret, placeholder)
	}
	return s
}

// URLQueryParam redacts the value of a specific query parameter (e.g.
// "key") in a URL string, without needing to know the secret value ahead
// of time. This catches cases where the secret itself isn't available at
// the call site but its position in the URL is known.
func URLQueryParam(rawURL, paramName string) string {
	marker := paramName + "="
	idx := strings.Index(rawURL, marker)
	if idx < 0 {
		return rawURL
	}
	start := idx + len(marker)
	end := strings.IndexAny(rawURL[start:], "&#")
	if end < 0 {
		return rawURL[:start] + placeholder
	}
	return rawURL[:start] + placeholder + rawURL[start+end:]
}
