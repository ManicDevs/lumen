package output

import "strings"

const placeholder = "[REDACTED]"

// Secrets replaces every occurrence of any secret string in s with
// [REDACTED]. Empty secrets are silently ignored.
func Secrets(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		s = strings.ReplaceAll(s, secret, placeholder)
	}
	return s
}

// URLQueryParam redacts the value of a named query parameter in a URL
// string, replacing it with [REDACTED].
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
