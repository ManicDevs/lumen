// Package dataset manages a lightweight, file-based dataset repository for
// collecting and curating LLM interaction frames from self-play generation
// sessions. Each session produces a "commit" of datapoints stored as JSON
// under data/datasets/commits/, with a ref pointer tracking the latest
// commit. The package also exposes RunTrain to produce a customised Ollama
// model from collected commits, and export utilities for external training.
package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/config"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/harvest"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

// DefaultHarvestTimeout is the default timeout for LLM requests during
// self-play generation and evaluation.
const DefaultHarvestTimeout = 120 * time.Second

var courtesyPhrases = []string{
	"thank you for", "you're welcome", "i appreciate", "i'm glad",
	"feel free to ask", "i'm here to help", "keep up the good work",
	"glad i could help",
}

func isLowContent(resp string) bool {
	lower := strings.ToLower(resp)
	hasCourtesy := false
	for _, p := range courtesyPhrases {
		if strings.Contains(lower, p) {
			hasCourtesy = true
			break
		}
	}
	return hasCourtesy && !strings.Contains(resp, "###")
}

func isDegenerateRepeat(datapoints []Datapoint) bool {
	if len(datapoints) < 2 {
		return false
	}
	norm := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}
	last := datapoints[len(datapoints)-1].Response
	prev := datapoints[len(datapoints)-2].Response
	if norm(last) != "" && norm(last) == norm(prev) {
		return true
	}
	return isLowContent(last) && isLowContent(prev)
}

func writeCommit(commitsDir, refsPath, model string, datapoints []Datapoint) (string, error) {
	canonical, err := json.Marshal(datapoints)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	commitID := fmt.Sprintf("%x", sum)

	now := time.Now().Format(time.RFC3339)
	commit := Commit{
		CommitID:   commitID,
		Timestamp:  now,
		Model:      model,
		Datapoints: datapoints,
	}
	commitData, err := json.MarshalIndent(commit, "", "  ")
	if err != nil {
		return "", err
	}
	commitPath := filepath.Join(commitsDir, "commit_"+commitID+".json")
	if err := os.WriteFile(commitPath, commitData, 0644); err != nil {
		return "", err
	}

	total := 1
	if existing, err := os.ReadFile(refsPath); err == nil {
		var ref RefPointer
		if json.Unmarshal(existing, &ref) == nil {
			total = ref.TotalCommits + 1
		}
	}
	ref := RefPointer{LatestCommit: commitID, LastUpdated: now, TotalCommits: total}
	refData, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(refsPath, refData, 0644); err != nil {
		return "", err
	}
	return commitID, nil
}

// GenerateOptions configures a self-play data generation run.
type GenerateOptions struct {
	Model         string
	Host          string
	Continuous    bool
	PipeDataset   bool
	Topic         string
	TargetPath    string
	MaxIterations int
	UseHarvest    bool
	Logger        *slog.Logger
}

// RunGenerate executes a self-play data generation session, producing
// prompt-response datapoints and optionally committing them to the dataset.
func RunGenerate(opts GenerateOptions) error {
	if opts.Topic == "" {
		opts.Topic = DefaultSeedTopics[rand.Intn(len(DefaultSeedTopics))]
	}
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 1
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	commitsDir := filepath.Join(DatasetRoot, "commits")
	stageDir := filepath.Join(DatasetRoot, "stage")
	refsPath := filepath.Join(DatasetRoot, "refs", "heads", "master")
	stagePath := filepath.Join(stageDir, "current.json")

	if opts.PipeDataset {
		if err := os.MkdirAll(commitsDir, 0755); err != nil {
			return fmt.Errorf("generate: creating commits dir: %w", err)
		}
		if err := os.MkdirAll(stageDir, 0755); err != nil {
			return fmt.Errorf("generate: creating stage dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(refsPath), 0755); err != nil {
			return fmt.Errorf("generate: creating refs dir: %w", err)
		}
	}

	var contextBlock string
	if opts.UseHarvest && opts.TargetPath != "" {
		ctx, err := harvest.Context(opts.TargetPath)
		if err != nil {
			opts.Logger.Warn("harvest failed, using topic only", "err", err)
		} else if ctx != "" {
			contextBlock = ctx
			opts.Logger.Info("harvested code context", "bytes", len(ctx))
		}
	}

	retryCfg := retry.Config{}
	if opts.Continuous {
		retryCfg.MaxAttempts = 3
	}

	// Limit harvest context to avoid token overflow
	const maxHarvestChars = 4000
	if len(contextBlock) > maxHarvestChars {
		contextBlock = contextBlock[:maxHarvestChars] + "\n\n[TRUNCATED - context too large]"
		opts.Logger.Info("truncated harvest context", "original_bytes", len(contextBlock), "max", maxHarvestChars)
	}

	eng := llm.NewLocalEngine(opts.Host, opts.Model, SystemPrompt, config.DefaultOllamaNumCtx, DefaultHarvestTimeout, retryCfg, opts.Logger)

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n[!] Interrupted. Finalizing commit...")
		cancel()
	}()
	defer signal.Stop(sigChan)

	fmt.Println("[LUMEN SELF-PLAY DATA GENERATION]")
	if opts.Continuous {
		fmt.Println("Running continuous self-chained generation... Press Ctrl+C to stop.")
	} else {
		fmt.Println("Running single-pass generation...")
	}
	if opts.PipeDataset {
		fmt.Printf("Committing collected frames under: %s\n", commitsDir)
	}
	if contextBlock != "" {
		fmt.Printf("Code context: %s (%d bytes)\n", opts.TargetPath, len(contextBlock))
	}
	fmt.Println("----------------------------------------------------------------")

	var datapoints []Datapoint
	var currentPrompt string

	if contextBlock != "" {
		currentPrompt = fmt.Sprintf("Analyze this codebase for bugs, performance issues, and security vulnerabilities:\n\n%s", contextBlock)
	} else {
		currentPrompt = opts.Topic
	}
	started := time.Now()

	for iter := 1; iter <= opts.MaxIterations; iter++ {
		select {
		case <-ctx.Done():
			fmt.Println("\n[!] Context cancelled")
			goto loopEnd
		default:
		}

		if opts.Continuous && iter > 1 {
			fmt.Printf("\n[auto-continue iteration %d/%d]\n", iter, opts.MaxIterations)
		}

		fmt.Printf("[prompt]: %s\n", truncateForDisplay(currentPrompt, 200))
		fmt.Print("[response]: ")

		reply, err := eng.Send(ctx, []llm.ChatMessage{{Role: "user", Content: currentPrompt}}, func(token string) {
			fmt.Print(token)
		})
		fmt.Println()

		if reply != "" {
			datapoints = append(datapoints, Datapoint{Prompt: currentPrompt, Response: reply})
			if opts.PipeDataset {
				if data, jsonErr := json.MarshalIndent(datapoints, "", "  "); jsonErr == nil {
					_ = os.WriteFile(stagePath, data, 0644)
				}
			}
		}

		if err != nil {
			fmt.Printf("[!] Model call failed, stopping run: %v\n", err)
			break
		}

		if opts.Continuous {
			currentPrompt = reply
			if isDegenerateRepeat(datapoints) {
				fmt.Println("[!] Self-chain collapsed into non-generative loop — stopping run.")
				break
			}
		} else {
			break
		}
	}
loopEnd:

	var totalChars int
	for _, dp := range datapoints {
		totalChars += len(dp.Response)
	}
	fmt.Println("----------------------------------------------------------------")
	fmt.Printf("[RUN CONCLUDED] %d datapoint(s), %s elapsed, %d response chars generated\n",
		len(datapoints), time.Since(started).Round(time.Second), totalChars)

	if !opts.PipeDataset || len(datapoints) == 0 {
		return nil
	}

	commitID, err := writeCommit(commitsDir, refsPath, opts.Model, datapoints)
	if err != nil {
		return fmt.Errorf("generate: finalizing commit: %w", err)
	}
	_ = os.Remove(stagePath)
	fmt.Printf("Committed %d frame(s) as %s\n", len(datapoints), commitID)

	return nil
}

func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}

// ExportDataset writes all collected datapoints to the specified output path
// in the given format. Supported formats are "sharegpt", "alpaca", and "jsonl".
func ExportDataset(format, outputPath string) error {
	commitsDir := filepath.Join(DatasetRoot, "commits")
	freshPaths, err := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	if err != nil {
		return fmt.Errorf("export: listing commits: %w", err)
	}
	trainedDir := filepath.Join(commitsDir, "trained")
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
	allPaths := append(freshPaths, archivedPaths...)

	if len(allPaths) == 0 {
		return errors.New("no commits found to export")
	}

	var allDatapoints []Datapoint
	for _, p := range allPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit Commit
		if err := json.Unmarshal(data, &commit); err != nil {
			continue
		}
		allDatapoints = append(allDatapoints, commit.Datapoints...)
	}

	if len(allDatapoints) == 0 {
		return errors.New("no datapoints found in commits")
	}

	switch strings.ToLower(format) {
	case "sharegpt":
		return exportShareGPT(allDatapoints, outputPath)
	case "alpaca":
		return exportAlpaca(allDatapoints, outputPath)
	case "jsonl":
		return exportJSONL(allDatapoints, outputPath)
	default:
		return fmt.Errorf("unknown export format: %s (supported: sharegpt, alpaca, jsonl)", format)
	}
}

func exportShareGPT(dps []Datapoint, path string) error {
	type ShareGPTMessage struct {
		From  string `json:"from"`
		Value string `json:"value"`
	}
	type ShareGPTConversation struct {
		ID       string            `json:"id"`
		Messages []ShareGPTMessage `json:"conversations"`
		System   string            `json:"system,omitempty"`
	}

	var conversations []ShareGPTConversation
	for i, dp := range dps {
		conv := ShareGPTConversation{
			ID: fmt.Sprintf("lumen-%d", i),
			Messages: []ShareGPTMessage{
				{From: "human", Value: dp.Prompt},
				{From: "gpt", Value: dp.Response},
			},
			System: SystemPrompt,
		}
		conversations = append(conversations, conv)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(conversations)
}

func exportAlpaca(dps []Datapoint, path string) error {
	type AlpacaEntry struct {
		Instruction string `json:"instruction"`
		Input       string `json:"input"`
		Output      string `json:"output"`
		System      string `json:"system,omitempty"`
	}

	var entries []AlpacaEntry
	for _, dp := range dps {
		entries = append(entries, AlpacaEntry{
			Instruction: "Analyze the following code/system for issues",
			Input:       dp.Prompt,
			Output:      dp.Response,
			System:      SystemPrompt,
		})
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func exportJSONL(dps []Datapoint, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, dp := range dps {
		if err := enc.Encode(dp); err != nil {
			return err
		}
	}
	return nil
}

// CurateDataset filters collected datapoints by removing low-content,
// too-short, too-long, and duplicate entries. It returns the number of
// removed and remaining datapoints.
func CurateDataset(minLength, maxLength int, dedupThreshold float64) (int, int, error) {
	commitsDir := filepath.Join(DatasetRoot, "commits")
	freshPaths, err := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	if err != nil {
		return 0, 0, fmt.Errorf("curate: listing commits: %w", err)
	}
	trainedDir := filepath.Join(commitsDir, "trained")
	archivedPaths, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
	allPaths := append(freshPaths, archivedPaths...)

	if len(allPaths) == 0 {
		return 0, 0, nil
	}

	var allDatapoints []Datapoint
	for _, p := range allPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit Commit
		if err := json.Unmarshal(data, &commit); err != nil {
			continue
		}
		allDatapoints = append(allDatapoints, commit.Datapoints...)
	}

	originalCount := len(allDatapoints)

	filtered := allDatapoints[:0]
	seen := make(map[string]bool)

	for _, dp := range allDatapoints {
		respLen := len(dp.Response)
		if minLength > 0 && respLen < minLength {
			continue
		}
		if maxLength > 0 && respLen > maxLength {
			continue
		}
		if isLowContent(dp.Response) {
			continue
		}

		norm := strings.ToLower(strings.Join(strings.Fields(dp.Response), " "))
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(norm)))[:16]
		if seen[hash] {
			continue
		}
		seen[hash] = true
		filtered = append(filtered, dp)
	}

	filteredCount := len(filtered)
	removed := originalCount - filteredCount

	if removed > 0 {
		fmt.Printf("[Curator] Filtered %d datapoints (low content, length, or duplicates)\n", removed)
		fmt.Printf("[Curator] Remaining: %d datapoints\n", filteredCount)
	}

	newCommitsDir := filepath.Join(DatasetRoot, "commits_curated")
	stageDir := filepath.Join(DatasetRoot, "stage")
	refsPath := filepath.Join(DatasetRoot, "refs", "heads", "master_curated")
	_ = os.MkdirAll(newCommitsDir, 0755)
	_ = os.MkdirAll(stageDir, 0755)
	_ = os.MkdirAll(filepath.Dir(refsPath), 0755)

	commitID, err := writeCommit(newCommitsDir, refsPath, "curated", filtered)
	if err != nil {
		return removed, filteredCount, err
	}
	fmt.Printf("[Curator] Created curated commit: %s\n", commitID)

	return removed, filteredCount, nil
}

// EvaluationResult holds the result of evaluating a single prompt against a model.
type EvaluationResult struct {
	Model           string
	Prompt          string
	Response        string
	LatencyMS       int64
	TokensPerSecond float64
	Score           float64
}

// EvaluationOptions configures a model evaluation run.
type EvaluationOptions struct {
	Model     string
	Host      string
	Prompts   []string
	BaseModel string
	Logger    *slog.Logger
}

// EvaluateModel runs a set of prompts against the specified model and returns
// per-prompt evaluation results including latency and a heuristic quality score.
func EvaluateModel(opts EvaluationOptions) ([]EvaluationResult, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.BaseModel == "" {
		opts.BaseModel = "qwen2.5-coder:3b"
	}

	retryCfg := retry.Config{}
	eng := llm.NewLocalEngine(opts.Host, opts.Model, SystemPrompt, config.DefaultOllamaNumCtx, DefaultHarvestTimeout, retryCfg, opts.Logger)

	ctx := context.Background()
	var results []EvaluationResult

	for _, prompt := range opts.Prompts {
		start := time.Now()
		reply, err := eng.Send(ctx, []llm.ChatMessage{{Role: "user", Content: prompt}}, nil)
		latency := time.Since(start)

		if err != nil {
			opts.Logger.Error("evaluation failed", "prompt", prompt, "err", err)
			results = append(results, EvaluationResult{
				Model:  opts.Model,
				Prompt: prompt,
				Score:  0,
			})
			continue
		}

		// Simple scoring: response length, presence of code blocks, structured content
		score := ScoreResponse(reply)

		results = append(results, EvaluationResult{
			Model:           opts.Model,
			Prompt:          prompt,
			Response:        reply,
			LatencyMS:       latency.Milliseconds(),
			TokensPerSecond: float64(len(reply)/4) / (float64(latency.Milliseconds()) / 1000),
			Score:           score,
		})
	}

	return results, nil
}

// ScoreResponse returns a heuristic quality score for an LLM response.
func ScoreResponse(response string) float64 {
	score := 0.0
	// Length bonus (capped)
	score += math.Min(float64(len(response))/1000, 2.0)
	// Code blocks
	if strings.Contains(response, "```") {
		score += 1.0
	}
	// Structured sections
	if strings.Contains(response, "###") {
		score += 1.0
	}
	// Specific technical terms
	techTerms := []string{"function", "struct", "interface", "goroutine", "channel", "mutex", "alloc", "latency", "throughput", "bottleneck", "optimization", "concurrency", "race condition", "deadlock"}
	for _, term := range techTerms {
		if strings.Contains(strings.ToLower(response), term) {
			score += 0.2
		}
	}
	return score
}

// CompareModels evaluates two models on the same prompts and returns their
// average scores keyed by model name.
func CompareModels(host, modelA, modelB string, prompts []string) (map[string]float64, error) {
	resultsA, err := EvaluateModel(EvaluationOptions{Model: modelA, Host: host, Prompts: prompts})
	if err != nil {
		return nil, fmt.Errorf("evaluate model A: %w", err)
	}
	resultsB, err := EvaluateModel(EvaluationOptions{Model: modelB, Host: host, Prompts: prompts})
	if err != nil {
		return nil, fmt.Errorf("evaluate model B: %w", err)
	}

	avgA := averageScore(resultsA)
	avgB := averageScore(resultsB)

	return map[string]float64{
		modelA: avgA,
		modelB: avgB,
	}, nil
}

func averageScore(results []EvaluationResult) float64 {
	if len(results) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range results {
		sum += r.Score
	}
	return sum / float64(len(results))
}

// DefaultEvalPrompts is a set of default prompts used for model evaluation.
var (
	DefaultEvalPrompts = []string{
		"Review this Go function for potential race conditions and memory leaks",
		"Identify SQL injection vulnerabilities in this code snippet",
		"Optimize this hot path for allocation reduction",
		"Explain the difference between buffered and unbuffered channels in Go",
		"Design a rate limiter with token bucket algorithm",
		"Find the bug in this concurrent map access pattern",
	}
)
