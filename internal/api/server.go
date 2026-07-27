package api

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
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
	mux.HandleFunc("GET /dashboard", s.handleUIDashboard)
	mux.HandleFunc("GET /datasets", s.handleUIDatasets)
	mux.HandleFunc("GET /generate", s.handleUIGenerate)
	mux.HandleFunc("GET /models", s.handleUIModels)
	mux.HandleFunc("GET /eval", s.handleUIEval)
	mux.HandleFunc("GET /train", s.handleUITrain)
	mux.HandleFunc("GET /prompts", s.handleUIPrompts)
	mux.HandleFunc("GET /git", s.handleUIGit)
	mux.HandleFunc("GET /versions", s.handleUIVersions)
	mux.HandleFunc("GET /models-manage", s.handleUIModelsManage)
	mux.HandleFunc("GET /training", s.handleUITrainingJobs)
	mux.HandleFunc("GET /audit", s.handleUIAudit)
	mux.HandleFunc("GET /webhooks", s.handleUIWebhooks)
	mux.HandleFunc("GET /schedules", s.handleUISchedules)
	mux.HandleFunc("GET /settings", s.handleUISettings)

	// API routes
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/prompts", s.handleListPrompts)
	mux.HandleFunc("POST /api/prompts/render", s.handleRenderPrompt)
	mux.HandleFunc("POST /api/git/commit", s.handleGitCommit)
	mux.HandleFunc("GET /api/git/status", s.handleGitStatus)
	mux.HandleFunc("GET /api/evals", s.handleListEvals)

	// Dataset versioning routes
	mux.HandleFunc("GET /api/dataset/versions", s.handleListVersions)
	mux.HandleFunc("POST /api/dataset/versions", s.handleCreateVersion)
	mux.HandleFunc("POST /api/dataset/versions/{id}/rollback", s.handleRollbackVersion)
	mux.HandleFunc("GET /api/dataset/versions/{from}/diff/{to}", s.handleDiffVersions)

	// Batch routes
	mux.HandleFunc("POST /api/datasets/batch", s.handleBatchGenerate)
	mux.HandleFunc("POST /api/models/eval/batch", s.handleBatchEval)

	// Enterprise: Dataset management with pagination/filtering
	mux.HandleFunc("GET /api/v1/datasets", s.handleListDatasetsV1)
	mux.HandleFunc("GET /api/v1/datasets/{id}", s.handleGetDatasetV1)
	mux.HandleFunc("DELETE /api/v1/datasets/{id}", s.handleDeleteDatasetV1)
	mux.HandleFunc("POST /api/v1/datasets/export", s.handleExportDatasetV1)
	mux.HandleFunc("POST /api/v1/datasets/import", s.handleImportDatasetV1)

	// Enterprise: Model management
	mux.HandleFunc("GET /api/v1/models", s.handleListModelsV1)
	mux.HandleFunc("GET /api/v1/models/{name}", s.handleGetModelV1)
	mux.HandleFunc("POST /api/v1/models/pull", s.handlePullModelV1)
	mux.HandleFunc("DELETE /api/v1/models/{name}", s.handleDeleteModelV1)
	mux.HandleFunc("POST /api/v1/models/copy", s.handleCopyModelV1)
	mux.HandleFunc("POST /api/v1/models/tags", s.handleTagModelV1)
	mux.HandleFunc("GET /api/v1/models/{name}/tags", s.handleListModelTagsV1)

	// Enterprise: Evaluation management
	mux.HandleFunc("GET /api/v1/evals", s.handleListEvalsV1)
	mux.HandleFunc("GET /api/v1/evals/{id}", s.handleGetEvalV1)
	mux.HandleFunc("DELETE /api/v1/evals/{id}", s.handleDeleteEvalV1)
	mux.HandleFunc("POST /api/v1/evals/compare", s.handleCompareEvalsV1)

	// Enterprise: Training management
	mux.HandleFunc("GET /api/v1/training/jobs", s.handleListTrainingJobsV1)
	mux.HandleFunc("POST /api/v1/training/jobs", s.handleCreateTrainingJobV1)
	mux.HandleFunc("GET /api/v1/training/jobs/{id}", s.handleGetTrainingJobV1)
	mux.HandleFunc("DELETE /api/v1/training/jobs/{id}", s.handleCancelTrainingJobV1)
	mux.HandleFunc("GET /api/v1/training/jobs/{id}/logs", s.handleTrainingJobLogsV1)

	// Enterprise: Audit log
	mux.HandleFunc("GET /api/v1/audit", s.handleListAuditV1)
	mux.HandleFunc("GET /api/v1/audit/{id}", s.handleGetAuditV1)

	// Enterprise: Webhooks
	mux.HandleFunc("GET /api/v1/webhooks", s.handleListWebhooksV1)
	mux.HandleFunc("POST /api/v1/webhooks", s.handleCreateWebhookV1)
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", s.handleDeleteWebhookV1)
	mux.HandleFunc("GET /api/v1/webhooks/{id}/deliveries", s.handleWebhookDeliveriesV1)

	// Enterprise: Scheduled jobs
	mux.HandleFunc("GET /api/v1/schedules", s.handleListSchedulesV1)
	mux.HandleFunc("POST /api/v1/schedules", s.handleCreateScheduleV1)
	mux.HandleFunc("GET /api/v1/schedules/{id}", s.handleGetScheduleV1)
	mux.HandleFunc("DELETE /api/v1/schedules/{id}", s.handleDeleteScheduleV1)

	// Enterprise: API Keys
	mux.HandleFunc("GET /api/v1/apikeys", s.handleListAPIKeysV1)
	mux.HandleFunc("POST /api/v1/apikeys", s.handleCreateAPIKeyV1)
	mux.HandleFunc("DELETE /api/v1/apikeys/{id}", s.handleDeleteAPIKeyV1)

	// Enterprise: Real-time events (SSE)
	mux.HandleFunc("GET /api/v1/events", s.handleEventsV1)

	// Enterprise: System info
	mux.HandleFunc("GET /api/v1/system/info", s.handleSystemInfoV1)
	mux.HandleFunc("GET /api/v1/system/health", s.handleSystemHealthV1)

	// Catch-all for unmatched /api/ routes — return JSON errors, not HTML redirects.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, "not found", http.StatusNotFound)
	})
	// Catch-all for unmatched UI routes — return JSON errors for non-existent paths.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

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

// writeJSONError sends a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": msg, "code": code})
}

// decodeBody decodes a JSON request body into dst. Returns an error if the
// body is empty or contains invalid JSON, using JSON-formatted error responses.
func decodeBody(r *http.Request, dst any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("empty request body")
	}
	return json.Unmarshal(body, dst)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
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
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Format == "" || req.Path == "" {
		writeJSONError(w, "format and path required", http.StatusBadRequest)
		return
	}
	if err := dataset.ExportDataset(req.Format, req.Path); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	decodeBody(r, &req)
	removed, kept, err := dataset.CurateDataset(req.MinLength, req.MaxLength, 0.85)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.BaseModel == "" {
		req.BaseModel = s.cfg.OllamaModel
	}
	if err := dataset.RunTrain(s.cfg.OllamaHost, req.BaseModel, req.UseAll); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.AdapterPath == "" || req.ModelName == "" {
		writeJSONError(w, "adapter_path and model_name required", http.StatusBadRequest)
		return
	}
	if req.BaseModel == "" {
		req.BaseModel = s.cfg.OllamaModel
	}

	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx := r.Context()

	digest, err := client.CreateBlob(ctx, req.AdapterPath)
	if err != nil {
		writeJSONError(w, "upload adapter: "+err.Error(), http.StatusInternalServerError)
		return
	}

	createReq := ollama.CreateRequest{
		Model:    req.ModelName,
		From:     req.BaseModel,
		Adapters: map[string]string{"adapter": digest},
	}
	if err := client.Create(ctx, createReq); err != nil {
		writeJSONError(w, "create model: "+err.Error(), http.StatusInternalServerError)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Model == "" || req.BaseModel == "" {
		writeJSONError(w, "model and base_model required", http.StatusBadRequest)
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
		writeJSONError(w, "eval not found", http.StatusNotFound)
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
		writeJSONError(w, "streaming unsupported", http.StatusInternalServerError)
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

// handleStats returns dashboard data as JSON.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	freshPaths, _ := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	trainedDir := filepath.Join(commitsDir, "trained")
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))

	var totalCommits int
	var totalDatapoints int
	var latestCommit map[string]any
	latestTime := time.Time{}

	for _, p := range append(freshPaths, archivedPaths...) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit dataset.Commit
		if json.Unmarshal(data, &commit) != nil {
			continue
		}
		totalCommits++
		totalDatapoints += len(commit.Datapoints)
		ts, _ := time.Parse(time.RFC3339, commit.Timestamp)
		if ts.After(latestTime) {
			latestTime = ts
			latestCommit = map[string]any{
				"commit_id":  commit.CommitID,
				"timestamp":  commit.Timestamp,
				"model":      commit.Model,
				"datapoints": len(commit.Datapoints),
			}
		}
	}

	// Count models.
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	modelResp, _ := client.List(ctx)
	modelCount := len(modelResp.Models)

	// System health.
	ctx2, cancel2 := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel2()
	ollamaOK := true
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, s.cfg.OllamaHost+"/api/tags", nil)
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			ollamaOK = false
		}
		if resp != nil {
			resp.Body.Close()
		}
	} else {
		ollamaOK = false
	}

	uptime := time.Since(s.startedAt).Truncate(time.Second).String()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total_commits":    totalCommits,
		"total_datapoints": totalDatapoints,
		"model_count":      modelCount,
		"requests_total":   metricsRequestsTotal.Load(),
		"errors_total":     metricsErrorsTotal.Load(),
		"git_commits":      metricsGitCommits.Load(),
		"eval_runs":        metricsEvalRuns.Load(),
		"ollama_ok":        ollamaOK,
		"disk_ok":          true,
		"dataset_ok":       true,
		"version":          version.Version,
		"uptime":           uptime,
		"latest_commit":    latestCommit,
	})
}

// handleListEvals returns all evaluation results as JSON.
func (s *Server) handleListEvals(w http.ResponseWriter, r *http.Request) {
	evalMu.RLock()
	defer evalMu.RUnlock()

	evals := make([]map[string]any, 0, len(evalStore))
	for id, result := range evalStore {
		evals = append(evals, map[string]any{
			"id":         id,
			"model_a":    result.ModelA,
			"model_b":    result.ModelB,
			"status":     result.Status,
			"started_at": result.StartedAt,
			"results":    result.Results,
		})
	}

	json.NewEncoder(w).Encode(map[string]any{
		"total": len(evals),
		"evals": evals,
	})
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := prompt.Get(req.Template)
	if !ok {
		writeJSONError(w, "unknown template: "+req.Template, http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"rendered": t.Render(req.Code)})
}

type GitCommitRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	var req GitCommitRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		writeJSONError(w, "message is required", http.StatusBadRequest)
		return
	}
	sha, err := git.CommitDataset(r.Context(), dataset.DatasetRoot, req.Message)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("git commit failed: %v", err), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "committed", "sha": sha})
}

func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	status, err := git.GetStatus(r.Context(), dataset.DatasetRoot)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]int{
		"staged":    status.Staged,
		"modified":  status.Modified,
		"untracked": status.Untracked,
	})
}

func (s *Server) handleUIDashboard(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "dashboard",
		PageTitle: "Dashboard",
		Version:   version.Version,
	}
	s.renderTemplate(w, "dashboard.html", data)
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
		PageTitle    string
		Version      string
		Commits      []map[string]any
		TotalCommits int
		Models       []any
	}{
		Page:         "datasets",
		PageTitle:    "Datasets",
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
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "generate",
		PageTitle: "Generate Data",
		Version:   version.Version,
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
		PageTitle string
		Version   string
		Templates []prompt.Template
		Selected  string
		Rendered  string
	}{
		Page:      "prompts",
		PageTitle: "Prompts",
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
		PageTitle string
		Version   string
		Staged    int
		Modified  int
		Untracked int
		IsRepo    bool
	}{
		Page:      "git",
		PageTitle: "Git",
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
		Page      string
		PageTitle string
		Version   string
		Models    []any
	}{
		Page:      "models",
		PageTitle: "Models",
		Version:   version.Version,
		Models:    models,
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
		Page      string
		PageTitle string
		Version   string
		Models    []any
	}{
		Page:      "eval",
		PageTitle: "Evaluation",
		Version:   version.Version,
		Models:    models,
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
		Page      string
		PageTitle string
		Version   string
		Models    []any
	}{
		Page:      "train",
		PageTitle: "Train",
		Version:   version.Version,
		Models:    models,
	}
	s.renderTemplate(w, "train.html", data)
}

func (s *Server) handleUIVersions(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "versions",
		PageTitle: "Versions",
		Version:   version.Version,
	}
	s.renderTemplate(w, "versions.html", data)
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
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Tag == "" {
		writeJSONError(w, "tag is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Tag, "v") {
		req.Tag = "v" + req.Tag
	}
	v, err := dataset.CreateVersion(dataset.DatasetRoot, req.Tag, req.Message)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleRollbackVersion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, "version id is required", http.StatusBadRequest)
		return
	}
	if err := dataset.RollbackTo(dataset.DatasetRoot, id); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "rolled_back", "version": id})
}

func (s *Server) handleDiffVersions(w http.ResponseWriter, r *http.Request) {
	from := r.PathValue("from")
	to := r.PathValue("to")
	if from == "" || to == "" {
		writeJSONError(w, "from and to version ids are required", http.StatusBadRequest)
		return
	}
	diff, err := dataset.DiffVersions(dataset.DatasetRoot, from, to)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Jobs) == 0 {
		writeJSONError(w, "jobs array is required", http.StatusBadRequest)
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
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ModelA == "" || req.ModelB == "" {
		writeJSONError(w, "model_a and model_b required", http.StatusBadRequest)
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

// ============================================================
// Enterprise V1 API Handlers
// ============================================================

// Dataset V1 Handlers with pagination/filtering

type DatasetListRequest struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Search   string `json:"search"`
	SortBy   string `json:"sort_by"`
	SortDesc bool   `json:"sort_desc"`
	Model    string `json:"model"`
}

func (s *Server) handleListDatasetsV1(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	pageSize := 20
	if p := q.Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if ps := q.Get("page_size"); ps != "" {
		fmt.Sscanf(ps, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

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
			"id":          commit.CommitID,
			"timestamp":   commit.Timestamp,
			"model":       commit.Model,
			"datapoints":  len(commit.Datapoints),
			"commit_json": string(data),
		})
	}

	// Filter
	if model := q.Get("model"); model != "" {
		filtered := commits[:0]
		for _, c := range commits {
			if c["model"] == model {
				filtered = append(filtered, c)
			}
		}
		commits = filtered
	}
	if search := q.Get("search"); search != "" {
		filtered := commits[:0]
		search = strings.ToLower(search)
		for _, c := range commits {
			id := strings.ToLower(fmt.Sprint(c["id"]))
			model := strings.ToLower(fmt.Sprint(c["model"]))
			if strings.Contains(id, search) || strings.Contains(model, search) {
				filtered = append(filtered, c)
			}
		}
		commits = filtered
	}

	// Sort
	sortBy := q.Get("sort_by")
	if sortBy == "" {
		sortBy = "timestamp"
	}
	sortDesc := q.Get("sort_desc") == "true"
	sort.Slice(commits, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "id":
			less = fmt.Sprint(commits[i]["id"]) < fmt.Sprint(commits[j]["id"])
		case "model":
			less = fmt.Sprint(commits[i]["model"]) < fmt.Sprint(commits[j]["model"])
		case "datapoints":
			less = fmt.Sprint(commits[i]["datapoints"]) < fmt.Sprint(commits[j]["datapoints"])
		default:
			less = fmt.Sprint(commits[i]["timestamp"]) < fmt.Sprint(commits[j]["timestamp"])
		}
		if sortDesc {
			return !less
		}
		return less
	})

	total := len(commits)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	json.NewEncoder(w).Encode(map[string]any{
		"data":  commits[start:end],
		"page":  page,
		"size":  pageSize,
		"total": total,
		"pages": (total + pageSize - 1) / pageSize,
	})
}

func (s *Server) handleGetDatasetV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	trainedDir := filepath.Join(commitsDir, "trained")

	for _, dir := range []string{commitsDir, trainedDir} {
		path := filepath.Join(dir, "commit_"+id+".json")
		data, err := os.ReadFile(path)
		if err == nil {
			var commit dataset.Commit
			if json.Unmarshal(data, &commit) == nil {
				json.NewEncoder(w).Encode(map[string]any{
					"data": commit,
				})
				return
			}
		}
	}
	writeJSONError(w, "dataset not found", http.StatusNotFound)
}

func (s *Server) handleDeleteDatasetV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	trainedDir := filepath.Join(commitsDir, "trained")

	deleted := false
	for _, dir := range []string{commitsDir, trainedDir} {
		path := filepath.Join(dir, "commit_"+id+".json")
		if err := os.Remove(path); err == nil {
			deleted = true
		}
	}
	if !deleted {
		writeJSONError(w, "dataset not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})
}

type ExportDatasetV1Request struct {
	Format string   `json:"format"` // json, csv, jsonl
	IDs    []string `json:"ids"`
	All    bool     `json:"all"`
	Model  string   `json:"model"`
	Output string   `json:"output"` // download, path
	Path   string   `json:"path"`
}

func (s *Server) handleExportDatasetV1(w http.ResponseWriter, r *http.Request) {
	var req ExportDatasetV1Request
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Format == "" {
		req.Format = "json"
	}

	commitsDir := filepath.Join(dataset.DatasetRoot, "commits")
	trainedDir := filepath.Join(commitsDir, "trained")
	freshPaths, _ := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
	allPaths := append(freshPaths, archivedPaths...)

	var allCommits []dataset.Commit
	for _, p := range allPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit dataset.Commit
		if json.Unmarshal(data, &commit) == nil {
			if req.All || len(req.IDs) == 0 || slices.Contains(req.IDs, commit.CommitID) {
				if req.Model == "" || commit.Model == req.Model {
					allCommits = append(allCommits, commit)
				}
			}
		}
	}

	var output []byte
	var filename string
	var contentType string

	switch req.Format {
	case "csv":
		contentType = "text/csv"
		filename = "lumen-datasets-" + time.Now().Format("20060102-150405") + ".csv"
		var b strings.Builder
		b.WriteString("commit_id,timestamp,model,datapoints\n")
		for _, c := range allCommits {
			b.WriteString(fmt.Sprintf("%s,%s,%s,%d\n", c.CommitID, c.Timestamp, c.Model, len(c.Datapoints)))
		}
		output = []byte(b.String())
	case "jsonl":
		contentType = "application/jsonl"
		filename = "lumen-datasets-" + time.Now().Format("20060102-150405") + ".jsonl"
		var parts []string
		for _, c := range allCommits {
			for _, dp := range c.Datapoints {
				data, _ := json.Marshal(dp)
				parts = append(parts, string(data))
			}
		}
		output = []byte(strings.Join(parts, "\n"))
	default: // json
		contentType = "application/json"
		filename = "lumen-datasets-" + time.Now().Format("20060102-150405") + ".json"
		output, _ = json.MarshalIndent(map[string]any{
			"exported_at": time.Now().Format(time.RFC3339),
			"total":       len(allCommits),
			"commits":     allCommits,
		}, "", "  ")
	}

	if req.Output == "path" && req.Path != "" {
		if err := os.WriteFile(req.Path, output, 0644); err != nil {
			writeJSONError(w, "write failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "exported", "path": req.Path})
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Write(output)
}

func (s *Server) handleImportDatasetV1(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data  []dataset.Commit `json:"data"`
		Path  string           `json:"path"`
		Force bool             `json:"force"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Data) == 0 && req.Path == "" {
		writeJSONError(w, "data or path required", http.StatusBadRequest)
		return
	}

	var commits []dataset.Commit
	if len(req.Data) > 0 {
		commits = req.Data
	} else {
		data, err := os.ReadFile(req.Path)
		if err != nil {
			writeJSONError(w, "read path: "+err.Error(), http.StatusBadRequest)
			return
		}
		var importData map[string]any
		if json.Unmarshal(data, &importData) == nil {
			if arr, ok := importData["commits"].([]any); ok {
				for _, item := range arr {
					if m, ok := item.(map[string]any); ok {
						b, _ := json.Marshal(m)
						var c dataset.Commit
						json.Unmarshal(b, &c)
						commits = append(commits, c)
					}
				}
			}
		}
	}

	imported := 0
	for _, c := range commits {
		if c.CommitID == "" {
			c.CommitID = fmt.Sprintf("%x", time.Now().UnixNano())
		}
		c.Timestamp = time.Now().Format(time.RFC3339)
		path := filepath.Join(dataset.DatasetRoot, "commits", "commit_"+c.CommitID+".json")
		data, _ := json.MarshalIndent(c, "", "  ")
		if err := os.WriteFile(path, data, 0644); err == nil {
			imported++
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"status": "imported", "count": imported})
}

// Model V1 Handlers

func (s *Server) handleListModelsV1(w http.ResponseWriter, r *http.Request) {
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := client.List(ctx)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	models := make([]map[string]any, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = map[string]any{
			"name":        m.Name,
			"model":       m.Model,
			"size":        m.Size,
			"digest":      m.Digest,
			"modified_at": m.ModifiedAt,
			"details":     m.Details,
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"models": models, "total": len(models)})
}

func (s *Server) handleGetModelV1(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := client.Show(ctx, ollama.ShowRequest{Model: name})
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(resp)
}

type PullModelRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

func (s *Server) handlePullModelV1(w http.ResponseWriter, r *http.Request) {
	var req PullModelRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		writeJSONError(w, "name required", http.StatusBadRequest)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx := r.Context()
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)
		err := client.PullStream(ctx, ollama.PullRequest{Model: req.Name}, func(progress ollama.PullProgressChunk) error {
			data, _ := json.Marshal(progress)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return nil
		})
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\":%q}\n\n", err.Error())
			flusher.Flush()
		}
		return
	}
	err := client.Pull(ctx, ollama.PullRequest{Model: req.Name})
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "pulled", "model": req.Name})
}

func (s *Server) handleDeleteModelV1(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := client.Delete(ctx, name)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "model": name})
}

type CopyModelRequest struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

func (s *Server) handleCopyModelV1(w http.ResponseWriter, r *http.Request) {
	var req CopyModelRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Source == "" || req.Target == "" {
		writeJSONError(w, "source and target required", http.StatusBadRequest)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := client.Copy(ctx, req.Source, req.Target)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "copied", "source": req.Source, "target": req.Target})
}

type TagModelRequest struct {
	Model string `json:"model"`
	Tag   string `json:"tag"`
}

func (s *Server) handleTagModelV1(w http.ResponseWriter, r *http.Request) {
	var req TagModelRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Model == "" || req.Tag == "" {
		writeJSONError(w, "model and tag required", http.StatusBadRequest)
		return
	}
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err := client.Create(ctx, ollama.CreateRequest{Model: req.Tag, From: req.Model})
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "tagged", "model": req.Model, "tag": req.Tag})
}

func (s *Server) handleListModelTagsV1(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	client := ollama.NewClient(s.cfg.OllamaHost)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := client.List(ctx)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var tags []string
	for _, m := range resp.Models {
		if strings.HasPrefix(m.Name, name+":") {
			tags = append(tags, m.Name)
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"model": name, "tags": tags, "total": len(tags)})
}

// Evaluation V1 Handlers

func (s *Server) handleListEvalsV1(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	pageSize := 20
	if p := q.Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if ps := q.Get("page_size"); ps != "" {
		fmt.Sscanf(ps, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	evalMu.RLock()
	evals := make([]map[string]any, 0, len(evalStore))
	for id, result := range evalStore {
		evals = append(evals, map[string]any{
			"id":         id,
			"model_a":    result.ModelA,
			"model_b":    result.ModelB,
			"status":     result.Status,
			"started_at": result.StartedAt,
			"completed":  result.Status == "completed",
		})
	}
	evalMu.RUnlock()

	sort.Slice(evals, func(i, j int) bool {
		return fmt.Sprint(evals[i]["started_at"]) > fmt.Sprint(evals[j]["started_at"])
	})

	total := len(evals)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	json.NewEncoder(w).Encode(map[string]any{
		"data":  evals[start:end],
		"page":  page,
		"size":  pageSize,
		"total": total,
		"pages": (total + pageSize - 1) / pageSize,
	})
}

func (s *Server) handleGetEvalV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	evalMu.RLock()
	result, ok := evalStore[id]
	evalMu.RUnlock()
	if !ok {
		writeJSONError(w, "eval not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleDeleteEvalV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	evalMu.Lock()
	_, ok := evalStore[id]
	if ok {
		delete(evalStore, id)
	}
	evalMu.Unlock()
	if !ok {
		writeJSONError(w, "eval not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})
}

type CompareEvalsRequest struct {
	EvalIDs []string `json:"eval_ids"`
}

func (s *Server) handleCompareEvalsV1(w http.ResponseWriter, r *http.Request) {
	var req CompareEvalsRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.EvalIDs) < 2 {
		writeJSONError(w, "at least 2 eval IDs required", http.StatusBadRequest)
		return
	}

	evalMu.RLock()
	results := make([]*EvalResult, 0, len(req.EvalIDs))
	for _, id := range req.EvalIDs {
		if r, ok := evalStore[id]; ok {
			results = append(results, r)
		}
	}
	evalMu.RUnlock()

	if len(results) < 2 {
		writeJSONError(w, "not enough valid evals found", http.StatusNotFound)
		return
	}

	comparison := map[string]any{
		"evals": make([]map[string]any, len(results)),
	}
	for i, r := range results {
		comparison["evals"].([]map[string]any)[i] = map[string]any{
			"id":         r.ID,
			"model_a":    r.ModelA,
			"model_b":    r.ModelB,
			"status":     r.Status,
			"started_at": r.StartedAt,
			"avg_score_a": func() float64 {
				if len(r.Results) == 0 {
					return 0
				}
				sum := 0.0
				for _, rr := range r.Results {
					sum += rr.ScoreA
				}
				return sum / float64(len(r.Results))
			}(),
			"avg_score_b": func() float64 {
				if len(r.Results) == 0 {
					return 0
				}
				sum := 0.0
				for _, rr := range r.Results {
					sum += rr.ScoreB
				}
				return sum / float64(len(r.Results))
			}(),
		}
	}
	json.NewEncoder(w).Encode(comparison)
}

// Training Jobs V1 Handlers

type TrainingJob struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"` // fewshot, lora
	Status      string         `json:"status"`
	Model       string         `json:"model"`
	BaseModel   string         `json:"base_model"`
	Config      map[string]any `json:"config"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Error       string         `json:"error,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
	Logs        []string       `json:"logs,omitempty"`
}

var (
	trainingJobs = make(map[string]*TrainingJob)
	trainingMu   sync.RWMutex
)

func (s *Server) handleListTrainingJobsV1(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	pageSize := 20
	if p := q.Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if ps := q.Get("page_size"); ps != "" {
		fmt.Sscanf(ps, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	trainingMu.RLock()
	jobs := make([]*TrainingJob, 0, len(trainingJobs))
	for _, job := range trainingJobs {
		jobs = append(jobs, job)
	}
	trainingMu.RUnlock()

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	total := len(jobs)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	json.NewEncoder(w).Encode(map[string]any{
		"data":  jobs[start:end],
		"page":  page,
		"size":  pageSize,
		"total": total,
		"pages": (total + pageSize - 1) / pageSize,
	})
}

type CreateTrainingJobRequest struct {
	Type      string         `json:"type"` // fewshot, lora
	BaseModel string         `json:"base_model"`
	Config    map[string]any `json:"config"`
}

func (s *Server) handleCreateTrainingJobV1(w http.ResponseWriter, r *http.Request) {
	var req CreateTrainingJobRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Type != "fewshot" && req.Type != "lora" {
		writeJSONError(w, "type must be 'fewshot' or 'lora'", http.StatusBadRequest)
		return
	}
	if req.BaseModel == "" {
		writeJSONError(w, "base_model required", http.StatusBadRequest)
		return
	}

	id := fmt.Sprintf("train-%d", time.Now().UnixNano())
	job := &TrainingJob{
		ID:        id,
		Type:      req.Type,
		Status:    "pending",
		BaseModel: req.BaseModel,
		Config:    req.Config,
		CreatedAt: time.Now(),
		Logs:      []string{},
	}
	trainingMu.Lock()
	trainingJobs[id] = job
	trainingMu.Unlock()

	go s.runTrainingJob(job)

	json.NewEncoder(w).Encode(map[string]any{"job_id": id, "status": "pending"})
}

func (s *Server) runTrainingJob(job *TrainingJob) {
	trainingMu.Lock()
	job.Status = "running"
	now := time.Now()
	job.StartedAt = &now
	trainingMu.Unlock()

	var result map[string]any
	var err error

	if job.Type == "fewshot" {
		useAll := false
		if v, ok := job.Config["use_all"].(bool); ok {
			useAll = v
		}
		err = dataset.RunTrain(s.cfg.OllamaHost, job.BaseModel, useAll)
		if err == nil {
			result = map[string]any{"model": "lumen-tuned", "base_model": job.BaseModel}
		}
	} else {
		adapterPath, _ := job.Config["adapter_path"].(string)
		modelName, _ := job.Config["model_name"].(string)
		if adapterPath == "" || modelName == "" {
			err = fmt.Errorf("adapter_path and model_name required for lora")
		} else {
			client := ollama.NewClient(s.cfg.OllamaHost)
			ctx := context.Background()
			digest, e := client.CreateBlob(ctx, adapterPath)
			if e != nil {
				err = fmt.Errorf("upload adapter: %w", e)
			} else {
				createReq := ollama.CreateRequest{
					Model:    modelName,
					From:     job.BaseModel,
					Adapters: map[string]string{"adapter": digest},
				}
				e = client.Create(ctx, createReq)
				if e != nil {
					err = fmt.Errorf("create model: %w", e)
				} else {
					result = map[string]any{"model": modelName, "base_model": job.BaseModel, "adapter_digest": digest}
				}
			}
		}
	}

	trainingMu.Lock()
	defer trainingMu.Unlock()
	now = time.Now()
	job.CompletedAt = &now
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
	} else {
		job.Status = "completed"
		job.Result = result
	}
}

func (s *Server) handleGetTrainingJobV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	trainingMu.RLock()
	job, ok := trainingJobs[id]
	trainingMu.RUnlock()
	if !ok {
		writeJSONError(w, "job not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(job)
}

func (s *Server) handleCancelTrainingJobV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	trainingMu.Lock()
	job, ok := trainingJobs[id]
	if ok {
		if job.Status == "pending" || job.Status == "running" {
			job.Status = "cancelled"
		}
	}
	trainingMu.Unlock()
	if !ok {
		writeJSONError(w, "job not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled", "id": id})
}

func (s *Server) handleTrainingJobLogsV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	trainingMu.RLock()
	job, ok := trainingJobs[id]
	trainingMu.RUnlock()
	if !ok {
		writeJSONError(w, "job not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"id": id, "logs": job.Logs})
}

// Audit Log V1 Handlers

func (s *Server) handleListAuditV1(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	pageSize := 50
	if p := q.Get("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if ps := q.Get("page_size"); ps != "" {
		fmt.Sscanf(ps, "%d", &pageSize)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	// Return empty for now - audit log would need to be integrated
	json.NewEncoder(w).Encode(map[string]any{
		"data":  []any{},
		"page":  page,
		"size":  pageSize,
		"total": 0,
		"pages": 0,
	})
}

func (s *Server) handleGetAuditV1(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, "audit entry not found", http.StatusNotFound)
}

// Webhook V1 Handlers

type Webhook struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Secret    string    `json:"secret,omitempty"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	webhooks  = make(map[string]*Webhook)
	webhookMu sync.RWMutex
)

func (s *Server) handleListWebhooksV1(w http.ResponseWriter, r *http.Request) {
	webhookMu.RLock()
	hooks := make([]*Webhook, 0, len(webhooks))
	for _, h := range webhooks {
		hooks = append(hooks, h)
	}
	webhookMu.RUnlock()
	json.NewEncoder(w).Encode(map[string]any{"webhooks": hooks})
}

type CreateWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Secret string   `json:"secret"`
}

func (s *Server) handleCreateWebhookV1(w http.ResponseWriter, r *http.Request) {
	var req CreateWebhookRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		writeJSONError(w, "url required", http.StatusBadRequest)
		return
	}
	if len(req.Events) == 0 {
		req.Events = []string{"eval.completed", "training.completed", "dataset.created"}
	}
	id := fmt.Sprintf("wh-%d", time.Now().UnixNano())
	wh := &Webhook{
		ID:        id,
		URL:       req.URL,
		Events:    req.Events,
		Secret:    req.Secret,
		Active:    true,
		CreatedAt: time.Now(),
	}
	webhookMu.Lock()
	webhooks[id] = wh
	webhookMu.Unlock()
	json.NewEncoder(w).Encode(wh)
}

func (s *Server) handleDeleteWebhookV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	webhookMu.Lock()
	_, ok := webhooks[id]
	if ok {
		delete(webhooks, id)
	}
	webhookMu.Unlock()
	if !ok {
		writeJSONError(w, "webhook not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})
}

func (s *Server) handleWebhookDeliveriesV1(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"deliveries": []any{}})
}

// Schedule V1 Handlers

type Schedule struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Cron      string         `json:"cron"`
	Action    string         `json:"action"` // generate, train, eval
	Config    map[string]any `json:"config"`
	Active    bool           `json:"active"`
	LastRun   *time.Time     `json:"last_run,omitempty"`
	NextRun   *time.Time     `json:"next_run,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

var (
	schedules  = make(map[string]*Schedule)
	scheduleMu sync.RWMutex
)

func (s *Server) handleListSchedulesV1(w http.ResponseWriter, r *http.Request) {
	scheduleMu.RLock()
	sch := make([]*Schedule, 0, len(schedules))
	for _, s := range schedules {
		sch = append(sch, s)
	}
	scheduleMu.RUnlock()
	json.NewEncoder(w).Encode(map[string]any{"schedules": sch})
}

type CreateScheduleRequest struct {
	Name   string         `json:"name"`
	Cron   string         `json:"cron"`
	Action string         `json:"action"`
	Config map[string]any `json:"config"`
}

func (s *Server) handleCreateScheduleV1(w http.ResponseWriter, r *http.Request) {
	var req CreateScheduleRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Cron == "" || req.Action == "" {
		writeJSONError(w, "name, cron, and action required", http.StatusBadRequest)
		return
	}
	id := fmt.Sprintf("sch-%d", time.Now().UnixNano())
	sch := &Schedule{
		ID:        id,
		Name:      req.Name,
		Cron:      req.Cron,
		Action:    req.Action,
		Config:    req.Config,
		Active:    true,
		CreatedAt: time.Now(),
	}
	scheduleMu.Lock()
	schedules[id] = sch
	scheduleMu.Unlock()
	json.NewEncoder(w).Encode(sch)
}

func (s *Server) handleGetScheduleV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scheduleMu.RLock()
	sch, ok := schedules[id]
	scheduleMu.RUnlock()
	if !ok {
		writeJSONError(w, "schedule not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(sch)
}

func (s *Server) handleDeleteScheduleV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	scheduleMu.Lock()
	_, ok := schedules[id]
	if ok {
		delete(schedules, id)
	}
	scheduleMu.Unlock()
	if !ok {
		writeJSONError(w, "schedule not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})
}

// API Key V1 Handlers

type APIKey struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Key       string     `json:"key,omitempty"`
	Prefix    string     `json:"prefix"`
	Scopes    []string   `json:"scopes"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
}

var (
	apiKeys  = make(map[string]*APIKey)
	apiKeyMu sync.RWMutex
)

func (s *Server) handleListAPIKeysV1(w http.ResponseWriter, r *http.Request) {
	apiKeyMu.RLock()
	keys := make([]*APIKey, 0, len(apiKeys))
	for _, k := range apiKeys {
		keys = append(keys, k)
	}
	apiKeyMu.RUnlock()
	json.NewEncoder(w).Encode(map[string]any{"keys": keys})
}

type CreateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

func (s *Server) handleCreateAPIKeyV1(w http.ResponseWriter, r *http.Request) {
	var req CreateAPIKeyRequest
	if err := decodeBody(r, &req); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		writeJSONError(w, "name required", http.StatusBadRequest)
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"read", "write"}
	}
	id := fmt.Sprintf("key-%d", time.Now().UnixNano())
	key := "lmn_" + fmt.Sprintf("%x", time.Now().UnixNano())[:32]
	prefix := key[:8] + "..."
	k := &APIKey{
		ID:        id,
		Name:      req.Name,
		Key:       key,
		Prefix:    prefix,
		Scopes:    req.Scopes,
		Active:    true,
		CreatedAt: time.Now(),
	}
	apiKeyMu.Lock()
	apiKeys[id] = k
	apiKeyMu.Unlock()
	json.NewEncoder(w).Encode(k)
}

func (s *Server) handleDeleteAPIKeyV1(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	apiKeyMu.Lock()
	_, ok := apiKeys[id]
	if ok {
		delete(apiKeys, id)
	}
	apiKeyMu.Unlock()
	if !ok {
		writeJSONError(w, "api key not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id})
}

// SSE Events V1

func (s *Server) handleEventsV1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Send initial connection event
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"timestamp\":\"%s\"}\n\n", time.Now().Format(time.RFC3339))
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Send heartbeat with stats
			stats := map[string]any{
				"type":          "stats",
				"timestamp":     time.Now().Format(time.RFC3339),
				"requests":      metricsRequestsTotal.Load(),
				"errors":        metricsErrorsTotal.Load(),
				"evals":         metricsEvalRuns.Load(),
				"git_commits":   metricsGitCommits.Load(),
				"training_jobs": len(trainingJobs),
			}
			data, _ := json.Marshal(stats)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// System Info V1

func (s *Server) handleSystemInfoV1(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	json.NewEncoder(w).Encode(map[string]any{
		"version":       version.Version,
		"uptime":        time.Since(s.startedAt).Truncate(time.Second).String(),
		"go_version":    runtime.Version(),
		"go_os":         runtime.GOOS,
		"go_arch":       runtime.GOARCH,
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
		"mem_alloc":     mem.Alloc,
		"mem_sys":       mem.Sys,
		"mem_heap":      mem.HeapAlloc,
		"dataset_root":  dataset.DatasetRoot,
		"ollama_host":   s.cfg.OllamaHost,
		"ollama_model":  s.cfg.OllamaModel,
	})
}

func (s *Server) handleSystemHealthV1(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	ollamaOK := true
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.OllamaHost+"/api/tags", nil)
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			ollamaOK = false
		}
		if resp != nil {
			resp.Body.Close()
		}
	} else {
		ollamaOK = false
	}

	diskOK := true
	tmpPath := filepath.Join(dataset.DatasetRoot, ".healthz-probe")
	if f, err := os.Create(tmpPath); err != nil {
		diskOK = false
	} else {
		f.Close()
		os.Remove(tmpPath)
	}

	datasetOK := true
	if info, err := os.Stat(dataset.DatasetRoot); err != nil || !info.IsDir() {
		datasetOK = false
	}

	overall := "ok"
	if !ollamaOK || !diskOK || !datasetOK {
		overall = "degraded"
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status": overall,
		"checks": map[string]any{
			"ollama":  map[string]any{"status": map[bool]string{true: "ok", false: "down"}[ollamaOK]},
			"disk":    map[string]any{"status": map[bool]string{true: "ok", false: "down"}[diskOK]},
			"dataset": map[string]any{"status": map[bool]string{true: "ok", false: "down"}[datasetOK]},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleUIModelsManage(w http.ResponseWriter, r *http.Request) {
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
		Page      string
		PageTitle string
		Version   string
		Models    []any
	}{
		Page:      "models-manage",
		PageTitle: "Model Management",
		Version:   version.Version,
		Models:    models,
	}
	s.renderTemplate(w, "models-manage.html", data)
}

func (s *Server) handleUITrainingJobs(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "training",
		PageTitle: "Training Jobs",
		Version:   version.Version,
	}
	s.renderTemplate(w, "training.html", data)
}

func (s *Server) handleUIAudit(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "audit",
		PageTitle: "Audit Log",
		Version:   version.Version,
	}
	s.renderTemplate(w, "audit.html", data)
}

func (s *Server) handleUIWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "webhooks",
		PageTitle: "Webhooks",
		Version:   version.Version,
	}
	s.renderTemplate(w, "webhooks.html", data)
}

func (s *Server) handleUISchedules(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page      string
		PageTitle string
		Version   string
	}{
		Page:      "schedules",
		PageTitle: "Scheduled Jobs",
		Version:   version.Version,
	}
	s.renderTemplate(w, "schedules.html", data)
}

func (s *Server) handleUISettings(w http.ResponseWriter, r *http.Request) {
	if s.tmpl == nil {
		http.Error(w, "templates not loaded", http.StatusInternalServerError)
		return
	}
	data := struct {
		Page        string
		PageTitle   string
		Version     string
		Config      *config.Config
		DatasetRoot string
	}{
		Page:        "settings",
		PageTitle:   "Settings",
		Version:     version.Version,
		Config:      s.cfg,
		DatasetRoot: dataset.DatasetRoot,
	}
	s.renderTemplate(w, "settings.html", data)
}
