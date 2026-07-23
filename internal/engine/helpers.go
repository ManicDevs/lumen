package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxAPIErrorMsgLen caps how much of a backend's error text ever reaches
// the terminal or the returned error chain. Full raw bodies (which can run
// to several KB of JSON) are for debug logs, not the chat UI.
const maxAPIErrorMsgLen = 200

// apiErrorMessage turns a backend's raw HTTP error body into a short,
// human-readable line: "<backend>: <reason> (HTTP <code>)". It understands
// the common {"error":{"message":"..."}} and {"error":"..."} shapes used by
// Ollama and OpenAI-compatible servers (e.g. LM Studio). If the body
// doesn't match a known shape, it falls back to a truncated snippet rather
// than dumping the whole thing.
func apiErrorMessage(backend string, statusCode int, body []byte) string {
	reason := extractErrorReason(body)
	if reason == "" {
		reason = "no further details"
	}
	return fmt.Sprintf("%s: %s (HTTP %d)", backend, reason, statusCode)
}

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

	// Some servers nest the string directly under "error".
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
