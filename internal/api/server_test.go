package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/config"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/dataset"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{
		OllamaHost:     "http://localhost:11434",
		OllamaModel:    "test-model",
		APIPort:        "0",
		MaxRetries:     1,
		OllamaNumCtx:   512,
		SystemPrompt:   "test",
		RequestTimeout: 30,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return NewServer(cfg, logger)
}

func TestHandleHealth(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	// Liveness probe always returns 200.
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthLiveness(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("liveness: expected 200, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("liveness: expected status ok, got %s", body["status"])
	}
}

func TestHandleHealthFull(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if _, ok := body["status"]; !ok {
		t.Error("full health: expected status key in response")
	}
	if _, ok := body["subsystems"]; !ok {
		t.Error("full health: expected subsystems key in response")
	}
	if _, ok := body["version"]; !ok {
		t.Error("full health: expected version key in response")
	}
	if _, ok := body["uptime"]; !ok {
		t.Error("full health: expected uptime key in response")
	}
}

func TestHandleMetrics(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	s.handleMetrics(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "lumen_http_requests_total") {
		t.Error("expected lumen_http_requests_total in metrics")
	}
	if !strings.Contains(body, "lumen_git_commits_total") {
		t.Error("expected lumen_git_commits_total in metrics")
	}
	if !strings.Contains(body, "lumen_eval_runs_total") {
		t.Error("expected lumen_eval_runs_total in metrics")
	}
	if !strings.Contains(body, "lumen_dataset_datapoints_total") {
		t.Error("expected lumen_dataset_datapoints_total in metrics")
	}
	if !strings.Contains(body, "lumen_version") {
		t.Error("expected lumen_version in metrics")
	}
}

func TestHandleListPrompts(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/prompts", nil)
	w := httptest.NewRecorder()
	s.handleListPrompts(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	prompts := body["prompts"].([]any)
	if len(prompts) < 5 {
		t.Errorf("expected at least 5 prompts, got %d", len(prompts))
	}
}

func TestHandleRenderPrompt(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	body := strings.NewReader(`{"template":"code-review","code":"func main() {}"}`)
	req := httptest.NewRequest("POST", "/api/prompts/render", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleRenderPrompt(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["rendered"] == "" {
		t.Error("expected non-empty rendered output")
	}
}

func TestHandleRenderPromptUnknown(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	body := strings.NewReader(`{"template":"unknown","code":"x"}`)
	req := httptest.NewRequest("POST", "/api/prompts/render", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleRenderPrompt(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleRenderPromptBadJSON(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/prompts/render", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleRenderPrompt(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGitStatus(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/git/status", nil)
	w := httptest.NewRecorder()
	s.handleGitStatus(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]int
	json.NewDecoder(w.Body).Decode(&body)
	if _, ok := body["staged"]; !ok {
		t.Error("expected staged key in response")
	}
}

func TestHandleEvents(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	flusher := &flushResponse{ResponseRecorder: w}
	done := make(chan struct{})
	go func() {
		s.handleEvents(flusher, req)
		close(done)
	}()
	cancel()
	<-done
}

func TestWithCORS(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := s.withCORS(inner)
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header")
	}
}

type flushResponse struct {
	*httptest.ResponseRecorder
}

func (f *flushResponse) Flush() {}

func TestHandleUIRoot(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	s.handleUIRoot(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", w.Code)
	}
}

func TestScoreResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		minScore float64
	}{
		{"empty", "", 0},
		{"short", "ok", 0.001},
		{"code block", "```go\nfunc main() {}\n```", 1.0},
		{"headers", "### Bug found", 1.0},
		{"technical", "goroutine leak and race condition detected", 0.4},
		{"full", "```go\n### Analysis\nFound goroutine leak and deadlock in mutex", 2.4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := dataset.ScoreResponse(tt.input)
			if score < tt.minScore {
				t.Errorf("score %f < expected min %f", score, tt.minScore)
			}
		})
	}
}
