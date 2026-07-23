package output

import "strings"

const placeholder = "[REDACTED]"

func Secrets(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		s = strings.ReplaceAll(s, secret, placeholder)
	}
	return s
}

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
