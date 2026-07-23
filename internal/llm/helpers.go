package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxAPIErrorMsgLen = 200

// apiErrorMessage formats a human-readable error from an HTTP response.
func apiErrorMessage(backend string, statusCode int, body []byte) string {
	reason := extractErrorReason(body)
	if reason == "" {
		reason = "no further details"
	}
	return fmt.Sprintf("%s: %s (HTTP %d)", backend, reason, statusCode)
}

// extractErrorReason tries to parse a JSON error body into a short reason.
func extractErrorReason(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error.Message != "" {
			return truncate(parsed.Error.Message)
		}
		if parsed.Message != "" {
			return truncate(parsed.Message)
		}
	}
	var altShape struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &altShape); err == nil && altShape.Error != "" {
		return truncate(altShape.Error)
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	return truncate(trimmed)
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxAPIErrorMsgLen {
		return s
	}
	return s[:maxAPIErrorMsgLen] + "…"
}
