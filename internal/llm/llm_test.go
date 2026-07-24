package llm

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

func TestTruncate_Short(t *testing.T) {
	t.Parallel()
	if got := truncate("hello"); got != "hello" {
		t.Errorf("truncate(\"hello\") = %q", got)
	}
}

func TestTruncate_Empty(t *testing.T) {
	t.Parallel()
	if got := truncate(""); got != "" {
		t.Errorf("truncate(\"\") = %q", got)
	}
}

func TestTruncate_ExactBoundary(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("a", maxAPIErrorMsgLen)
	if got := truncate(s); got != s {
		t.Errorf("truncate at boundary should return unchanged string")
	}
}

func TestTruncate_Long(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("a", maxAPIErrorMsgLen+50)
	got := truncate(s)
	if len(got) != maxAPIErrorMsgLen+3 { // 200 + "…"
		t.Errorf("truncate long string: len = %d, want %d", len(got), maxAPIErrorMsgLen+3)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("truncated string should end with …")
	}
}

func TestExtractErrorReason_NestedError(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":{"message":"rate limit exceeded"}}`)
	got := extractErrorReason(body)
	if got != "rate limit exceeded" {
		t.Errorf("extractErrorReason nested = %q", got)
	}
}

func TestExtractErrorReason_TopLevelMessage(t *testing.T) {
	t.Parallel()
	body := []byte(`{"message":"bad request"}`)
	got := extractErrorReason(body)
	if got != "bad request" {
		t.Errorf("extractErrorReason message = %q", got)
	}
}

func TestExtractErrorReason_ErrorString(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":"something went wrong"}`)
	got := extractErrorReason(body)
	if got != "something went wrong" {
		t.Errorf("extractErrorReason error string = %q", got)
	}
}

func TestExtractErrorReason_PlainText(t *testing.T) {
	t.Parallel()
	body := []byte(`plain text error`)
	got := extractErrorReason(body)
	if got != "plain text error" {
		t.Errorf("extractErrorReason plain text = %q", got)
	}
}

func TestExtractErrorReason_Empty(t *testing.T) {
	t.Parallel()
	got := extractErrorReason([]byte{})
	if got != "" {
		t.Errorf("extractErrorReason empty = %q, want \"\"", got)
	}
	got = extractErrorReason(nil)
	if got != "" {
		t.Errorf("extractErrorReason nil = %q, want \"\"", got)
	}
}

func TestExtractErrorReason_MalformedJSON(t *testing.T) {
	t.Parallel()
	got := extractErrorReason([]byte(`{not json`))
	if got != "{not json" {
		t.Errorf("extractErrorReason malformed = %q", got)
	}
}

func TestApiErrorMessage_WithBody(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":{"message":"forbidden"}}`)
	got := apiErrorMessage("Ollama", 403, body)
	want := "Ollama: forbidden (HTTP 403)"
	if got != want {
		t.Errorf("apiErrorMessage = %q, want %q", got, want)
	}
}

func TestApiErrorMessage_EmptyBody(t *testing.T) {
	t.Parallel()
	got := apiErrorMessage("OpenAI", 500, nil)
	want := "OpenAI: no further details (HTTP 500)"
	if got != want {
		t.Errorf("apiErrorMessage = %q, want %q", got, want)
	}
}

func TestApiErrorMessage_TruncatesLongBody(t *testing.T) {
	t.Parallel()
	longMsg := strings.Repeat("x", 300)
	body := []byte(`{"error":{"message":"` + longMsg + `"}}`)
	got := apiErrorMessage("Backend", 500, body)
	if len(got) > 300 {
		t.Errorf("apiErrorMessage too long: %d chars", len(got))
	}
}

func TestNewOpenAIEngine_Defaults(t *testing.T) {
	t.Parallel()
	eng := NewOpenAIEngine("http://localhost:8080/", "model", "sys", 0, retry.Config{}, slog.Default())
	if eng.Name() != "OpenAI-compat" {
		t.Errorf("Name() = %q", eng.Name())
	}
	if eng.host != "http://localhost:8080" {
		t.Errorf("host should have trailing / stripped, got %q", eng.host)
	}
	if eng.idleTimeout != 60*time.Second {
		t.Errorf("idleTimeout should default to 60s, got %v", eng.idleTimeout)
	}
}

func TestNewOpenAIEngine_CustomTimeout(t *testing.T) {
	t.Parallel()
	eng := NewOpenAIEngine("http://host", "m", "sys", 30*time.Second, retry.Config{}, nil)
	if eng.idleTimeout != 30*time.Second {
		t.Errorf("idleTimeout = %v, want 30s", eng.idleTimeout)
	}
}

func TestNewLocalEngine_Defaults(t *testing.T) {
	t.Parallel()
	eng := NewLocalEngine("http://host", "model", "sys", 0, 0, retry.Config{}, slog.Default())
	if eng.Name() != "Ollama" {
		t.Errorf("Name() = %q", eng.Name())
	}
	if eng.numCtx != 8192 {
		t.Errorf("numCtx should default to 8192, got %d", eng.numCtx)
	}
	if eng.idleTimeout != 60*time.Second {
		t.Errorf("idleTimeout should default to 60s, got %v", eng.idleTimeout)
	}
}

func TestNewLocalEngine_CustomValues(t *testing.T) {
	t.Parallel()
	eng := NewLocalEngine("http://host", "model", "sys", 4096, 30*time.Second, retry.Config{}, nil)
	if eng.numCtx != 4096 {
		t.Errorf("numCtx = %d, want 4096", eng.numCtx)
	}
	if eng.idleTimeout != 30*time.Second {
		t.Errorf("idleTimeout = %v, want 30s", eng.idleTimeout)
	}
}

func TestNewLocalEngine_NegativeNumCtx(t *testing.T) {
	t.Parallel()
	eng := NewLocalEngine("http://host", "model", "sys", -1, 0, retry.Config{}, nil)
	if eng.numCtx != 8192 {
		t.Errorf("negative numCtx should default to 8192, got %d", eng.numCtx)
	}
}
