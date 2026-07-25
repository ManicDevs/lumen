package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/config"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/dataset"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/git"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/ollama"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/prompt"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/version"
)

//go:embed templates/*.html
var templateFS embed.FS

// Server is the HTTP API server for Lumen.
type Server struct {
	cfg            *config.Config
	logger         *slog.Logger
	server         *http.Server
	tmpl           *template.Template
	rateLimiter    *rateLimitState
	rateLimitRate  int   // requests per second per IP, default 100
	rateLimitBurst int   // burst capacity, default 200
	maxBodyBytes   int64 // max request body size, default 10MB
	startedAt      time.Time
}

func NewServer(cfg *config.Config, logger *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		cfg:            cfg,
		logger:         logger,
		rateLimitRate:  100,
		rateLimitBurst: 200,
		maxBodyBytes:   10 << 20, // 10MB
		startedAt:      time.Now(),
	}
	s.rateLimiter = newRateLimitState(float64(s.rateLimitRate), float64(s.rateLimitBurst))

	// Load templates with custom functions from embedded FS
	tmpl := template.New("").Funcs(template.FuncMap{
		"shortID": func(s string) string {
			if len(s) > 16 {
				return s[:16] + "..."
			}
			return s
		},
	})
	tmplSub, subErr := fs.Sub(templateFS, "templates")
	if subErr != nil {
		logger.Warn("could not get template sub-fs", "err", subErr)
	} else {
		parsed, parseErr := tmpl.ParseFS(tmplSub, "*.html")
		if parseErr != nil {
			logger.Warn("could not parse templates", "err", parseErr)
		} else {
			s.tmpl = parsed
		}
	}

	// Health endpoints
	mux.HandleFunc("GET /healthz", s.handleHealthLiveness)
	mux.HandleFunc("GET /readyz", s.handleHealthReadiness)
	mux.HandleFunc("GET /health", s.handleHealth)

	// API routes
	mux.HandleFunc("GET /api/datasets", s.handleListDatasets)
	mux.HandleFunc("POST /api/datasets/generate", s.handleGenerateDataset)
	mux.HandleFunc("POST /api/datasets/export", s.handleExportDataset)
	mux.HandleFunc("POST /api/datasets/curate", s.handleCurateDataset)
	mux.HandleFunc("GET /api/models", s.handleListModels)
	mux.HandleFunc("POST /api/models/train", s.handleTrainModel)
	mux.HandleFunc("POST /api/models/train-lora", s.handleTrainLoRA)
	mux.HandleFunc("POST /api/models/eval", s.handleEvalModel)
	mux.HandleFunc("GET /api/models/eval/{id}", s.handleEvalResult)
	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Static files (register first to avoid pattern conflicts)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("internal/api/static"))))

	// UI routes
	mux.HandleFunc("GET /", s.handleUIRoot)
	mux.HandleFunc("GET /datasets", s.handleUIDatasets)
	mux.HandleFunc("GET /generate", s.handleUIGenerate)
	mux.HandleFunc("GET /models", s.handleUIModels)
	mux.HandleFunc("GET /eval", s.handleUIEval)
	mux.HandleFunc("GET /train", s.handleUITrain)
	mux.HandleFunc("GET /prompts", s.handleUIPrompts)
	mux.HandleFunc("GET /git", s.handleUIGit)
	mux.HandleFunc("GET /versions", s.handleUIVersions)

	// API routes
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/prompts", s.handleListPrompts)
	mux.HandleFunc("POST /api/prompts/render", s.handleRenderPrompt)
	mux.HandleFunc("POST /api/git/commit", s.handleGitCommit)
	mux.HandleFunc("GET /api/git/status", s.handleGitStatus)

	// Dataset versioning routes
	mux.HandleFunc("GET /api/dataset/versions", s.handleListVersions)
	mux.HandleFunc("POST /api/dataset/versions", s.handleCreateVersion)
	mux.HandleFunc("POST /api/dataset/versions/{id}/rollback", s.handleRollbackVersion)
	mux.HandleFunc("GET /api/dataset/versions/{from}/diff/{to}", s.handleDiffVersions)

	// Batch routes
	mux.HandleFunc("POST /api/datasets/batch", s.handleBatchGenerate)
	mux.HandleFunc("POST /api/models/eval/batch", s.handleBatchEval)

	handler := s.withCORS(mux)
	handler = s.withSecurityHeaders(handler)
	handler = s.withRequestID(handler)
	handler = s.withCompression(handler)
	handler = s.withRecovery(handler)
	handler = s.withRateLimit(handler)
	handler = s.withBodyLimit(s.maxBodyBytes)(handler)
	handler = s.withLogging(handler)

	// Start rate limiter cleanup goroutine.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.rateLimiter.cleanup()
		}
	}()

	s.server = &http.Server{
		Addr:         ":" + cfg.APIPort,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	s.logger.Info("starting API server", "addr", s.server.Addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		metricsRequestsTotal.Add(1)
		metricsRequestDuration.Add(uint64(time.Since(start).Milliseconds()))
		if wrapped.status >= 400 {
			metricsErrorsTotal.Add(1)
		}
		if r.Method == "POST" && r.URL.Path == "/api/git/commit" && wrapped.status < 400 {
			metricsGitCommits.Add(1)
		}
		if r.Method == "POST" && r.URL.Path == "/api/models/eval" && wrapped.status < 400 {
			metricsEvalRuns.Add(1)
		}
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", wrapped.status, "duration", time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (s *Server) handleHealthLiveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleHealthReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	subsystems := map[string]string{}
	overall := "ok"

	// Check Ollama connectivity.
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	ollamaOK := true
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.OllamaHost+"/api/tags", nil)
	if err != nil {
		ollamaOK = false
	} else {
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			ollamaOK = false
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	if ollamaOK {
		subsystems["ollama"] = "ok"
	} else {
		subsystems["ollama"] = "down"
		overall = "degraded"
	}

	// Check dataset directory is writable.
	diskOK := true
	tmpPath := filepath.Join(dataset.DatasetRoot, ".healthz-probe")
	if f, err := os.Create(tmpPath); err != nil {
		diskOK = false
	} else {
		f.Close()
		os.Remove(tmpPath)
	}
	if diskOK {
		subsystems["disk"] = "ok"
	} else {
		subsystems["disk"] = "down"
		overall = "degraded"
	}

	// Check dataset directory exists.
	datasetOK := true
	if info, err := os.Stat(dataset.DatasetRoot); err != nil || !info.IsDir() {
		datasetOK = false
	}
	if datasetOK {
		subsystems["dataset"] = "ok"
	} else {
		subsystems["dataset"] = "down"
		overall = "down"
	}

	statusCode := http.StatusOK
	if overall == "down" {
		statusCode = http.StatusServiceUnavailable
	}
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"status":     overall,
		"subsystems": subsystems,
		"version":    version.Version,
		"uptime":     time.Since(s.startedAt).Truncate(time.Second).String(),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Combine liveness + readiness into a single full health response.
	w.Header().Set("Content-Type", "application/json")

	subsystems := map[string]string{}
	overall := "ok"

	// Ollama check.
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
	defer cancel()
	ollamaOK := true
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.OllamaHost+"/api/tags", nil)
	if err != nil {
		ollamaOK = false
	} else {
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			ollamaOK = false
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	if ollamaOK {
		subsystems["ollama"] = "ok"
	} else {
		subsystems["ollama"] = "down"
		overall = "degraded"
	}

	// Disk check.
	diskOK := true
	tmpPath := filepath.Join(dataset.DatasetRoot, ".healthz-probe")
	if f, err := os.Create(tmpPath); err != nil {
		diskOK = false
	} else {
		f.Close()
		os.Remove(tmpPath)
	}
	if diskOK {
		subsystems["disk"] = "ok"
	} else {
		subsystems["disk"] = "down"
		overall = "degraded"
	}

	// Dataset check.
	datasetOK := true
	if info, err := os.Stat(dataset.DatasetRoot); err != nil || !info.IsDir() {
		datasetOK = false
	}
	if datasetOK {
		subsystems["dataset"] = "ok"
	} else {
		subsystems["dataset"] = "down"
		overall = "down"
	}

	statusCode := http.StatusOK
	if overall == "down" {
		statusCode = http.StatusServiceUnavailable
	}
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"status":     overall,
		"subsystems": subsystems,
		"version":    version.Version,
		"uptime":     time.Since(s.startedAt).Truncate(time.Second).String(),
	})
}

func (s *Server) handleListDatasets(w http.ResponseWriter, r *http.Request) {
	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	freshPaths, _ := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	trainedDir := filepath.Join(commitsDir, "trained")
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))

	var commits []map[string]any
	for _, p := range append(freshPaths, archivedPaths...) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit dataset.Commit
		if json.Unmarshal(data, &commit) != nil {
			continue
		}
		commits = append(commits, map[string]any{
			"commit_id":  commit.CommitID,
			"timestamp":  commit.Timestamp,
			"model":      commit.Model,
			"datapoints": len(commit.Datapoints),
		})
	}

	json.NewEncoder(w).Encode(map[string]any{
		"total_commits": len(commits),
		"commits":       commits,
	})
}

type GenerateRequest struct {
	Model         string `json:"model"`
	Continuous    bool   `json:"continuous"`
	PipeDataset   bool   `json:"pipe_dataset"`
	Topic         string `json:"topic"`
	TargetPath    string `json:"target_path"`
	MaxIterations int    `json:"max_iterations"`
}

func (s *Server) handleGenerateDataset(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		req.Model = s.cfg.OllamaModel
	}
	if req.MaxIterations == 0 {
		req.MaxIterations = 1
	}

	opts := dataset.GenerateOptions{
		Model:         req.Model,
		Host:          s.cfg.OllamaHost,
		Continuous:    req.Continuous,
		PipeDataset:   req.PipeDataset,
		Topic:         req.Topic,
		TargetPath:    req.TargetPath,
		MaxIterations: req.MaxIterations,
		UseHarvest:    req.TargetPath != "",
		Logger:        s.logger,
	}

	if err := dataset.RunGenerate(opts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

type ExportRequest struct {
	Format string `json:"format"`
	Path   string `json:"path"`
}

func (s *Server) handleExportDataset(w http.ResponseWriter, r *http.Request) {
	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Format == "" || req.Path == "" {
		http.Error(w, "format and path required", http.StatusBadRequest)
		return
	}
	if err := dataset.ExportDataset(req.Format, req.Path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "exported", "path": req.Path})
}

type CurateRequest struct {
	MinLength int `json:"min_length"`
	MaxLength int `json:"max_length"`
}

func (s *Server) handleCurateDataset(w http.ResponseWriter, r *http.Request) {
	var req CurateRequest
	json.NewDecoder(r.Body).Decode(&req)
	removed, kept, err := dataset.CurateDataset(req.MinLength, req.MaxLength, 0.85)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]int{"removed": removed, "kept": kept})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := client.List(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

type TrainRequest struct {
	BaseModel string `json:"base_model"`
	UseAll    bool   `json:"use_all"`
}

func (s *Server) handleTrainModel(w http.ResponseWriter, r *http.Request) {
	var req TrainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.BaseModel == "" {
		req.BaseModel = s.cfg.OllamaModel
	}
	if err := dataset.RunTrain(s.cfg.OllamaHost, req.BaseModel, req.UseAll); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "trained", "model": "lumen-tuned"})
}

type TrainLoRARequest struct {
	AdapterPath string `json:"adapter_path"`
	ModelName   string `json:"model_name"`
	BaseModel   string `json:"base_model"`
}

func (s *Server) handleTrainLoRA(w http.ResponseWriter, r *http.Request) {
	var req TrainLoRARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.AdapterPath == "" || req.ModelName == "" {
		http.Error(w, "adapter_path and model_name required", http.StatusBadRequest)
		return
	}
	if req.BaseModel == "" {
		req.BaseModel = s.cfg.OllamaModel
	}

	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx := r.Context()

	digest, err := client.CreateBlob(ctx, req.AdapterPath)
	if err != nil {
		http.Error(w, "upload adapter: "+err.Error(), http.StatusInternalServerError)
		return
	}

	createReq := ollama.CreateRequest{
		Model:    req.ModelName,
		From:     req.BaseModel,
		Adapters: map[string]string{"adapter": digest},
	}
	if err := client.Create(ctx, createReq); err != nil {
		http.Error(w, "create model: "+err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "created", "model": req.ModelName, "adapter_digest": digest})
}

type EvalRequest struct {
	Model     string   `json:"model"`
	BaseModel string   `json:"base_model"`
	Prompts   []string `json:"prompts"`
}

type EvalResult struct {
	ID        string                 `json:"id"`
	ModelA    string                 `json:"model_a"`
	ModelB    string                 `json:"model_b"`
	Results   []EvalComparisonResult `json:"results"`
	StartedAt time.Time              `json:"started_at"`
	Status    string                 `json:"status"`
}

type EvalComparisonResult struct {
	Prompt    string  `json:"prompt"`
	ResponseA string  `json:"response_a"`
	ResponseB string  `json:"response_b"`
	ScoreA    float64 `json:"score_a"`
	ScoreB    float64 `json:"score_b"`
	LatencyA  int64   `json:"latency_a_ms"`
	LatencyB  int64   `json:"latency_b_ms"`
}

var evalStore = make(map[string]*EvalResult)
var evalMu sync.RWMutex

func (s *Server) handleEvalModel(w http.ResponseWriter, r *http.Request) {
	var req EvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Model == "" || req.BaseModel == "" {
		http.Error(w, "model and base_model required", http.StatusBadRequest)
		return
	}
	if len(req.Prompts) == 0 {
		req.Prompts = []string{
			"Find bugs in this concurrent Go code.",
			"Identify security issues in this auth handler.",
			"Optimize this hot path for allocations.",
		}
	}

	evalID := fmt.Sprintf("eval-%d", time.Now().UnixNano())
	result := &EvalResult{
		ID:        evalID,
		ModelA:    req.Model,
		ModelB:    req.BaseModel,
		StartedAt: time.Now(),
		Status:    "running",
	}
	evalMu.Lock()
	evalStore[evalID] = result
	evalMu.Unlock()

	go func() {
		client := ollama.NewClient(s.cfg.OllamaHost)
		ctx := context.Background()
		var comparisons []EvalComparisonResult

		for _, prompt := range req.Prompts {
			for _, model := range []string{req.Model, req.BaseModel} {
				start := time.Now()
				resp, err := client.Generate(ctx, ollama.GenerateRequest{
					Model:   model,
					Prompt:  prompt,
					Stream:  false,
					Options: ollama.Options{NumPredict: 200, Temperature: 0.1},
				})
				latency := time.Since(start).Milliseconds()
				var response string
				if err != nil {
					response = "ERROR: " + err.Error()
				} else {
					response = resp.Response
				}
				score := dataset.ScoreResponse(response)

				if model == req.Model {
					comparisons = append(comparisons, EvalComparisonResult{
						Prompt:    prompt,
						ResponseA: response,
						ScoreA:    score,
						LatencyA:  latency,
					})
				} else {
					if len(comparisons) > 0 {
						comparisons[len(comparisons)-1].ResponseB = response
						comparisons[len(comparisons)-1].ScoreB = score
						comparisons[len(comparisons)-1].LatencyB = latency
					}
				}
			}
		}

		result.Results = comparisons
		result.Status = "completed"
	}()

	json.NewEncoder(w).Encode(map[string]string{"eval_id": evalID, "status": "started"})
}

func (s *Server) handleEvalResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	evalMu.RLock()
	result, ok := evalStore[id]
	evalMu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "data: {\"time\": \"%s\"}\n\n", time.Now().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

var (
	metricsRequestsTotal   atomic.Uint64
	metricsRequestDuration atomic.Uint64
	metricsErrorsTotal     atomic.Uint64
	metricsGitCommits      atomic.Uint64
	metricsEvalRuns        atomic.Uint64
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	fmt.Fprintf(w, "# HELP lumen_http_requests_total Total HTTP requests\n")
	fmt.Fprintf(w, "# TYPE lumen_http_requests_total counter\n")
	fmt.Fprintf(w, "lumen_http_requests_total %d\n", metricsRequestsTotal.Load())

	fmt.Fprintf(w, "# HELP lumen_http_request_duration_seconds Request duration in seconds\n")
	fmt.Fprintf(w, "# TYPE lumen_http_request_duration_seconds summary\n")
	fmt.Fprintf(w, "lumen_http_request_duration_seconds_sum %f\n", float64(metricsRequestDuration.Load())/1000.0)
	fmt.Fprintf(w, "lumen_http_request_duration_seconds_count %d\n", metricsRequestsTotal.Load())

	fmt.Fprintf(w, "# HELP lumen_http_errors_total Total HTTP errors\n")
	fmt.Fprintf(w, "# TYPE lumen_http_errors_total counter\n")
	fmt.Fprintf(w, "lumen_http_errors_total %d\n", metricsErrorsTotal.Load())

	fmt.Fprintf(w, "# HELP lumen_git_commits_total Total git commits\n")
	fmt.Fprintf(w, "# TYPE lumen_git_commits_total counter\n")
	fmt.Fprintf(w, "lumen_git_commits_total %d\n", metricsGitCommits.Load())

	fmt.Fprintf(w, "# HELP lumen_eval_runs_total Total evaluation runs\n")
	fmt.Fprintf(w, "# TYPE lumen_eval_runs_total counter\n")
	fmt.Fprintf(w, "lumen_eval_runs_total %d\n", metricsEvalRuns.Load())

	// Count datapoints across all commits.
	var datapoints int
	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	freshPaths, _ := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	trainedDir := filepath.Join(commitsDir, "trained")
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
	for _, p := range append(freshPaths, archivedPaths...) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit dataset.Commit
		if json.Unmarshal(data, &commit) == nil {
			datapoints += len(commit.Datapoints)
		}
	}
	fmt.Fprintf(w, "# HELP lumen_dataset_datapoints_total Total datapoints in dataset\n")
	fmt.Fprintf(w, "# TYPE lumen_dataset_datapoints_total gauge\n")
	fmt.Fprintf(w, "lumen_dataset_datapoints_total %d\n", datapoints)

	fmt.Fprintf(w, "# HELP lumen_version Lumen version\n")
	fmt.Fprintf(w, "# TYPE lumen_version gauge\n")
	fmt.Fprintf(w, "lumen_version{version=\"%s\"} 1\n", version.Version)
}

func (s *Server) handleListPrompts(w http.ResponseWriter, r *http.Request) {
	templates := prompt.List()
	json.NewEncoder(w).Encode(map[string]any{"prompts": templates})
}

type RenderRequest struct {
	Template string `json:"template"`
	Code     string `json:"code"`
}

func (s *Server) handleRenderPrompt(w http.ResponseWriter, r *http.Request) {
	var req RenderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := prompt.Get(req.Template)
	if !ok {
		http.Error(w, "unknown template: "+req.Template, http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"rendered": t.Render(req.Code)})
}

type GitCommitRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	var req GitCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sha, err := git.CommitDataset(r.Context(), dataset.DatasetRoot, req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "committed", "sha": sha})
}

func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	status, err := git.GetStatus(r.Context(), dataset.DatasetRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]int{
		"staged":    status.Staged,
		"modified":  status.Modified,
		"untracked": status.Untracked,
	})
}

func (s *Server) handleUIRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/datasets", http.StatusSeeOther)
}

func (s *Server) handleUIDatasets(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, _ := client.List(ctx)
	models := make([]any, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = m
	}
	commits := s.getCommits()
	data := struct {
		Page         string
		Version      string
		Commits      []map[string]any
		TotalCommits int
		Models       []any
	}{
		Page:         "datasets",
		Version:      version.Version,
		Commits:      commits,
		TotalCommits: len(commits),
		Models:       models,
	}
	s.renderTemplate(w, "datasets.html", data)
}

func (s *Server) handleUIGenerate(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page    string
		Version string
	}{
		Page:    "generate",
		Version: version.Version,
	}
	s.renderTemplate(w, "generate.html", data)
}

func (s *Server) handleUIPrompts(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	templates := prompt.List()
	data := struct {
		Page      string
		Version   string
		Templates []prompt.Template
		Selected  string
		Rendered  string
	}{
		Page:      "prompts",
		Version:   version.Version,
		Templates: templates,
	}
	s.renderTemplate(w, "prompts.html", data)
}

func (s *Server) handleUIGit(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	status, err := git.GetStatus(r.Context(), dataset.DatasetRoot)
	if err != nil {
		status = &git.Status{}
	}
	data := struct {
		Page      string
		Version   string
		Staged    int
		Modified  int
		Untracked int
		IsRepo    bool
	}{
		Page:      "git",
		Version:   version.Version,
		Staged:    status.Staged,
		Modified:  status.Modified,
		Untracked: status.Untracked,
		IsRepo:    git.IsRepo(r.Context(), dataset.DatasetRoot),
	}
	s.renderTemplate(w, "git.html", data)
}

func (s *Server) handleUIModels(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, _ := client.List(ctx)
	models := make([]any, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = m
	}
	data := struct {
		Page    string
		Version string
		Models  []any
	}{
		Page:    "models",
		Version: version.Version,
		Models:  models,
	}
	s.renderTemplate(w, "models.html", data)
}

func (s *Server) handleUIEval(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, _ := client.List(ctx)
	models := make([]any, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = m
	}
	data := struct {
		Page    string
		Version string
		Models  []any
	}{
		Page:    "eval",
		Version: version.Version,
		Models:  models,
	}
	s.renderTemplate(w, "eval.html", data)
}

func (s *Server) handleUITrain(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, _ := client.List(ctx)
	models := make([]any, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = m
	}
	data := struct {
		Page    string
		Version string
		Models  []any
	}{
		Page:    "train",
		Version: version.Version,
		Models:  models,
	}
	s.renderTemplate(w, "train.html", data)
}

func (s *Server) getCommits() []map[string]any {
	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	freshPaths, _ := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	trainedDir := filepath.Join(commitsDir, "trained")
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
	allPaths := append(freshPaths, archivedPaths...)

	var commits []map[string]any
	for _, p := range allPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit dataset.Commit
		if json.Unmarshal(data, &commit) != nil {
			continue
		}
		commits = append(commits, map[string]any{
			"CommitID":   commit.CommitID,
			"Timestamp":  commit.Timestamp,
			"Model":      commit.Model,
			"Datapoints": len(commit.Datapoints),
		})
	}
	return commits
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	pageContent, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		s.logger.Error("could not read page template", "name", name, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmplClone, err := s.tmpl.Clone()
	if err != nil {
		s.logger.Error("could not clone template", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmplClone, err = tmplClone.Parse(string(pageContent))
	if err != nil {
		s.logger.Error("could not parse page template", "name", name, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmplClone.ExecuteTemplate(w, "layout", data); err != nil {
		s.logger.Error("template execute error", "template", name, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := dataset.ListVersions(dataset.DatasetRoot)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"total":    len(versions),
		"versions": versions,
	})
}

type createVersionRequest struct {
	Tag     string `json:"tag"`
	Message string `json:"message"`
}

func (s *Server) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	var req createVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Tag == "" {
		http.Error(w, "tag is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Tag, "v") {
		req.Tag = "v" + req.Tag
	}
	v, err := dataset.CreateVersion(dataset.DatasetRoot, req.Tag, req.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleRollbackVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "version id is required", http.StatusBadRequest)
		return
	}
	if err := dataset.RollbackTo(dataset.DatasetRoot, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "rolled_back", "version": id})
}

func (s *Server) handleDiffVersions(w http.ResponseWriter, r *http.Request) {
	from := r.PathValue("from")
	to := r.PathValue("to")
	if from == "" || to == "" {
		http.Error(w, "from and to version ids are required", http.StatusBadRequest)
		return
	}
	diff, err := dataset.DiffVersions(dataset.DatasetRoot, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(diff)
}

// BatchGenerateRequest represents a batch dataset generation request.
type BatchGenerateRequest struct {
	Jobs     []GenerateRequest `json:"jobs"`
	Parallel int               `json:"parallel"` // max parallel jobs, default 2
}

// BatchJobResult holds the result of a single batch job.
type BatchJobResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// handleBatchGenerate processes multiple generation jobs concurrently.
func (s *Server) handleBatchGenerate(w http.ResponseWriter, r *http.Request) {
	var req BatchGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Jobs) == 0 {
		http.Error(w, "jobs array is required", http.StatusBadRequest)
		return
	}
	if req.Parallel <= 0 {
		req.Parallel = 2
	}
	if req.Parallel > 8 {
		req.Parallel = 8
	}

	results := make([]BatchJobResult, len(req.Jobs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, req.Parallel)

	for i, job := range req.Jobs {
		wg.Add(1)
		go func(idx int, j GenerateRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if j.Model == "" {
				j.Model = s.cfg.OllamaModel
			}
			if j.MaxIterations == 0 {
				j.MaxIterations = 1
			}

			opts := dataset.GenerateOptions{
				Model:         j.Model,
				Host:          s.cfg.OllamaHost,
				Continuous:    j.Continuous,
				PipeDataset:   j.PipeDataset,
				Topic:         j.Topic,
				TargetPath:    j.TargetPath,
				MaxIterations: j.MaxIterations,
				UseHarvest:    j.TargetPath != "",
				Logger:        s.logger,
			}

			err := dataset.RunGenerate(opts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				results[idx] = BatchJobResult{Index: idx, Status: "failed", Error: err.Error()}
			} else {
				results[idx] = BatchJobResult{Index: idx, Status: "completed"}
			}
		}(i, job)
	}

	wg.Wait()
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "completed",
		"results": results,
	})
}

// BatchEvalRequest represents a batch evaluation request.
type BatchEvalRequest struct {
	ModelA     string     `json:"model_a"`
	ModelB     string     `json:"model_b"`
	PromptSets [][]string `json:"prompt_sets"` // multiple prompt groups
	Parallel   int        `json:"parallel"`
}

// BatchEvalResult holds the result of a single batch eval round.
type BatchEvalResult struct {
	Index  int    `json:"index"`
	Status string `json:"status"`
	EvalID string `json:"eval_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// handleBatchEval runs multiple evaluation rounds concurrently.
func (s *Server) handleBatchEval(w http.ResponseWriter, r *http.Request) {
	var req BatchEvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ModelA == "" || req.ModelB == "" {
		http.Error(w, "model_a and model_b required", http.StatusBadRequest)
		return
	}
	if len(req.PromptSets) == 0 {
		req.PromptSets = [][]string{
			{
				"Find bugs in this concurrent Go code.",
				"Identify security issues in this auth handler.",
				"Optimize this hot path for allocations.",
			},
		}
	}
	if req.Parallel <= 0 {
		req.Parallel = 2
	}
	if req.Parallel > 8 {
		req.Parallel = 8
	}

	results := make([]BatchEvalResult, len(req.PromptSets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, req.Parallel)

	for i, prompts := range req.PromptSets {
		wg.Add(1)
		go func(idx int, p []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			evalID := fmt.Sprintf("eval-batch-%d-%d", time.Now().UnixNano(), idx)
			client := ollama.NewClient(s.cfg.OllamaHost)
			ctx := context.Background()
			var comparisons []EvalComparisonResult

			for _, prompt := range p {
				for _, model := range []string{req.ModelA, req.ModelB} {
					start := time.Now()
					resp, err := client.Generate(ctx, ollama.GenerateRequest{
						Model:   model,
						Prompt:  prompt,
						Stream:  false,
						Options: ollama.Options{NumPredict: 200, Temperature: 0.1},
					})
					latency := time.Since(start).Milliseconds()
					var response string
					if err != nil {
						response = "ERROR: " + err.Error()
					} else {
						response = resp.Response
					}
					score := dataset.ScoreResponse(response)

					if model == req.ModelA {
						comparisons = append(comparisons, EvalComparisonResult{
							Prompt:    prompt,
							ResponseA: response,
							ScoreA:    score,
							LatencyA:  latency,
						})
					} else {
						if len(comparisons) > 0 {
							comparisons[len(comparisons)-1].ResponseB = response
							comparisons[len(comparisons)-1].ScoreB = score
							comparisons[len(comparisons)-1].LatencyB = latency
						}
					}
				}
			}

			evalResult := &EvalResult{
				ID:        evalID,
				ModelA:    req.ModelA,
				ModelB:    req.ModelB,
				Results:   comparisons,
				StartedAt: time.Now(),
				Status:    "completed",
			}
			evalMu.Lock()
			evalStore[evalID] = evalResult
			evalMu.Unlock()

			mu.Lock()
			defer mu.Unlock()
			results[idx] = BatchEvalResult{Index: idx, Status: "completed", EvalID: evalID}
		}(i, prompts)
	}

	wg.Wait()
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "completed",
		"results": results,
	})
}

// handleUIVersions renders the versions management page.
func (s *Server) handleUIVersions(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page    string
		Version string
	}{
		Page:    "versions",
		Version: version.Version,
	}
	s.renderTemplate(w, "versions.html", data)
}
