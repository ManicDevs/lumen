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
	s.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/dashboard" {
		t.Errorf("redirect to /dashboard, got %s", loc)
	}
}

func TestHandleUIDashboard(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("expected 'Dashboard' in response")
	}
	if !strings.Contains(body, "lumen.css") {
		t.Error("expected lumen.css in response")
	}
}

func TestHandleUIAllPages(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	pages := []string{"/dashboard", "/datasets", "/generate", "/models", "/eval", "/train", "/prompts", "/git", "/versions"}
	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("GET", page, nil)
			w := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", page, w.Code)
			}
			body := w.Body.String()
			if !strings.Contains(body, "lumen.css") {
				t.Errorf("%s: missing CSS", page)
			}
			if !strings.Contains(body, "sidebar") {
				t.Errorf("%s: missing sidebar", page)
			}
		})
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

func TestAPIErrorReturnsJSON(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	tests := []struct {
		name            string
		method          string
		path            string
		body            string
		wantCode        int
		wantErrContains string
	}{
		{"eval empty body", "POST", "/api/models/eval", "", 400, "empty request body"},
		{"eval missing fields", "POST", "/api/models/eval", "{}", 400, "model and base_model required"},
		{"eval bad json", "POST", "/api/models/eval", "{bad", 400, "invalid character"},
		{"render empty body", "POST", "/api/prompts/render", "", 400, "empty request body"},
		{"train-lora missing fields", "POST", "/api/models/train-lora", "{}", 400, "adapter_path and model_name required"},
		{"git commit empty body", "POST", "/api/git/commit", "", 400, "empty request body"},
		{"batch empty jobs", "POST", "/api/datasets/batch", `{"jobs":[]}`, 400, "jobs array is required"},
		{"batch bad json", "POST", "/api/datasets/batch", "not json", 400, "invalid character"},
		{"create version empty", "POST", "/api/dataset/versions", "{}", 400, "tag is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var body *strings.Reader
			if tt.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tt.body)
			}
			req := httptest.NewRequest(tt.method, tt.path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantCode)
			}

			var resp map[string]any
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("response is not JSON: %v (body: %q)", err, w.Body.String())
			}
			if resp["error"] == nil {
				t.Error("response missing 'error' key")
			}
			if tt.wantErrContains != "" {
				errMsg, _ := resp["error"].(string)
				if !strings.Contains(errMsg, tt.wantErrContains) {
					t.Errorf("error %q does not contain %q", errMsg, tt.wantErrContains)
				}
			}
		})
	}
}

func TestAPINotFoundReturnsJSON(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp["error"] == nil {
		t.Error("response missing 'error' key")
	}
}

func TestAPIMethodNotAllowedReturnsJSON(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/git/commit", nil)
	w := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(w, req)

	// Go's ServeMux returns 404 for method mismatches, not 405.
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp["error"] == nil {
		t.Error("response missing 'error' key")
	}
}

func TestGitCommitRequiresMessage(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	body := strings.NewReader(`{"message":""}`)
	req := httptest.NewRequest("POST", "/api/git/commit", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleGitCommit(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] == nil {
		t.Error("response missing error key")
	}
}
