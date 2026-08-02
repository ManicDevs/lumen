// Package app wires together all of Lumen's subsystems — config loading,
// engine initialisation, session management, harvest, agent, dataset, and
// audit logging — and exposes a single Run() entrypoint that dispatches to
// interactive, autonomous, training, or dataset-initialisation modes based on
// the parsed command-line flags.
package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/agent"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/api"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/config"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/dataset"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/harvest"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/ollama"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/output"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

const progName = "lumen"

// Run is Lumen's top-level entrypoint. It parses flags, loads configuration,
// initialises the engine and audit log, then dispatches to the appropriate
// mode: code-review session, interactive chat, autonomous agent, training,
// or dataset initialisation. Returns an exit code (0 on success).
func Run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	flags := ParseFlags(args)

	cfg, err := config.Load(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] config error: %v\n", progName, err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] config invalid: %v\n", progName, err)
		return 1
	}

	logger := output.NewLogger(os.Stderr, cfg.LogFormat, cfg.LogLevel)

	ollamaHost := cfg.OllamaHost
	if ollamaHost == "" {
		ollamaHost = config.DefaultOllamaHost
	}

	// --- Mode dispatch: self-contained modes that don't need an engine.
	if flags.EasterEgg {
		return runEasterEgg(ollamaHost, flags, logger)
	}
	if flags.Train || flags.TrainAll {
		return runTrain(ollamaHost, cfg.OllamaModel, flags.TrainAll)
	}
	if flags.TrainLoRA {
		return runTrainLoRA(ollamaHost, flags, logger)
	}
	if flags.DatasetInit {
		return runDatasetInit()
	}
	if flags.DatasetExport {
		return runDatasetExport(flags)
	}
	if flags.DatasetCurate {
		return runDatasetCurate(flags)
	}
	if flags.Eval {
		return runEval(ollamaHost, flags, logger)
	}
	if flags.AutoLoop {
		return runAutoLoop(ollamaHost, flags, logger)
	}
	if flags.API {
		return runAPI(&cfg, logger)
	}

	retryCfg := retry.Config{
		MaxAttempts: cfg.MaxRetries,
		BaseDelay:   retry.DefaultConfig.BaseDelay,
		MaxDelay:    retry.DefaultConfig.MaxDelay,
	}

	auditLog, err := session.OpenAuditLog(cfg.AuditLogPath, logger)
	if err != nil {
		logger.Warn("could not open audit log, continuing without one", "err", err)
		auditLog = nil
	} else {
		defer auditLog.Close()
	}

	eng := llm.NewLocalEngine(ollamaHost, cfg.OllamaModel, cfg.SystemPrompt,
		cfg.OllamaNumCtx, cfg.RequestTimeout, retryCfg, logger)

	sendMessage := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		reply, err := eng.Send(ctx, history, onToken)
		return eng.Name(), reply, err
	}

	// --- Auto Mode.
	if flags.AutoMode {
		return runAuto(ctx, flags, eng, sendMessage, logger)
	}

	// --- Seed session with harvested code or chat context.
	initialContext := "chat context"
	if flags.TargetPath != "" {
		if err := harvest.ValidateTargetPath(flags.TargetPath); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] %v\n", progName, err)
			return 1
		}
		if err := createSnapshot(cfg.BackupDir, flags.TargetPath, "before"); err != nil {
			logger.Warn("could not snapshot before session", "err", err)
		}
		ctxBlock, err := harvest.Context(flags.TargetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] harvest error: %v\n", progName, err)
			return 1
		}
		if strings.TrimSpace(ctxBlock) == "" {
			fmt.Fprintf(os.Stderr, "[%s] no recognized source files found under %q\n", progName, flags.TargetPath)
			return 1
		}
		initialContext = ctxBlock
		fmt.Printf("%s: harvested %s\n", output.Bold("Lumen Code Mode"), flags.TargetPath)
	} else {
		fmt.Println(output.Bold("Lumen Chat Shell Initialized."))
	}

	hist := session.NewHistory(initialContext)
	runExchange := makeExchange(hist, sendMessage, auditLog, logger)

	if flags.TargetPath != "" {
		fmt.Print("\n[Lumen]: ")
		runExchange()
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "exit" || input == "quit" || input == "/exit" || input == "/quit" {
			fmt.Println("[Lumen] Graceful shutdown completed.")
			os.Exit(0)
			break
		}
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/auto") {
			goal := strings.TrimSpace(strings.TrimPrefix(input, "/auto"))
			opts := agent.Options{
				Goal:       goal,
				Sandbox:    flags.AutoSandbox,
				WorkDir:    ".",
				LiveOutput: flags.LiveOutput,
			}
			sendFunc := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
				return sendMessage(ctx, history, onToken)
			}
			notifyFunc := func(line string) {
				fmt.Println(line)
			}
			if err := agent.Run(ctx, opts, hist, sendFunc, notifyFunc); err != nil {
				fmt.Printf("auto agent error: %v\n", err)
			}
			continue
		}

		hist.Append(llm.ChatMessage{Role: "user", Content: input})
		if auditLog != nil {
			auditLog.Write(session.AuditEntry{Role: "user", TokenCount: session.ApproxTokens(input)})
		}
		fmt.Print("\n[Lumen]: ")
		runExchange()
	}

	if flags.TargetPath != "" {
		if err := createSnapshot(cfg.BackupDir, flags.TargetPath, "after"); err != nil {
			logger.Warn("could not snapshot after session", "err", err)
		}
	}
	return 0
}

// makeExchange returns a closure that sends the current conversation history
// to the LLM, streams the response tokens to stdout, and records the
// exchange in the history and audit log.
// TODO: pass cfg.RequestTimeout into this closure instead of hardcoding 5 minutes.
func makeExchange(hist *session.History, sendMessage func(context.Context, []llm.ChatMessage, llm.StreamFunc) (string, string, error), auditLog *session.AuditLog, logger *slog.Logger) func() {
	return func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		tokenChan := make(chan string, 100)
		var reply string
		var name string
		var err error

		go func() {
			defer close(tokenChan)
			name, reply, err = sendMessage(ctx, hist.Snapshot(), func(tok string) {
				tokenChan <- tok
			})
		}()

		for tok := range tokenChan {
			fmt.Print(tok)
		}
		dur := time.Since(start)
		if err != nil {
			fmt.Printf("\nError: %v\n", err)
			if auditLog != nil {
				auditLog.Write(session.AuditEntry{
					Role: "assistant", DurationMS: dur.Milliseconds(), Error: err.Error(),
				})
			}
			return
		}
		fmt.Println()
		hist.Append(llm.ChatMessage{Role: "assistant", Content: reply})
		if auditLog != nil {
			auditLog.Write(session.AuditEntry{
				Role: "assistant", TokenCount: session.ApproxTokens(reply),
				DurationMS: dur.Milliseconds(), Engine: name,
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Mode runners
// ---------------------------------------------------------------------------

// runEasterEgg starts the self-play dataset generation loop (--easter-egg).
func runEasterEgg(host string, flags Flags, logger *slog.Logger) int {
	opts := dataset.GenerateOptions{
		Model:         config.DefaultOllamaModel,
		Host:          host,
		Continuous:    flags.Continuous,
		PipeDataset:   flags.PipeDataset,
		Topic:         flags.CustomTopic,
		TargetPath:    flags.TargetPath,
		MaxIterations: 1,
		UseHarvest:    flags.TargetPath != "",
		Logger:        logger,
	}
	if flags.Continuous {
		opts.MaxIterations = 20
	}
	if err := dataset.RunGenerate(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Easter egg failed: %v\n", err)
		return 1
	}
	return 0
}

// runTrain creates a customised model from collected dataset commits.
func runTrain(host, baseModel string, trainAll bool) int {
	if err := dataset.RunTrain(host, baseModel, trainAll); err != nil {
		fmt.Fprintf(os.Stderr, "Training failed: %v\n", err)
		return 1
	}
	return 0
}

// runTrainLoRA creates a model with a LoRA adapter from a trained file.
func runTrainLoRA(host string, flags Flags, logger *slog.Logger) int {
	if flags.LoRAAdapter == "" {
		fmt.Fprintf(os.Stderr, "[%s] --lora-adapter required (path to .gguf LoRA adapter)\n", progName)
		return 1
	}
	if flags.LoRAModel == "" {
		fmt.Fprintf(os.Stderr, "[%s] --lora-model required (name for the new model, e.g. lumen-tuned-lora)\n", progName)
		return 1
	}
	baseModel := config.DefaultOllamaModel

	ollamaClient := ollama.NewClient(host)
	ctx := context.Background()

	// Upload LoRA adapter as blob
	digest, err := ollamaClient.CreateBlob(ctx, flags.LoRAAdapter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "LoRA upload failed: %v\n", err)
		return 1
	}
	logger.Info("uploaded LoRA adapter", "digest", digest)

	// Create model with adapter
	req := ollama.CreateRequest{
		Model:    flags.LoRAModel,
		From:     baseModel,
		Adapters: map[string]string{"adapter": digest},
	}
	if err := ollamaClient.Create(ctx, req); err != nil {
		fmt.Fprintf(os.Stderr, "LoRA model creation failed: %v\n", err)
		return 1
	}

	fmt.Printf("Created LoRA model %q from base %q with adapter %s\n", flags.LoRAModel, baseModel, digest)
	return 0
}

// runDatasetInit initialises the dataset directory structure.
func runDatasetInit() int {
	if err := dataset.RunInit(); err != nil {
		fmt.Fprintf(os.Stderr, "Dataset init failed: %v\n", err)
		return 1
	}
	return 0
}

// runDatasetExport exports the dataset to ShareGPT, Alpaca, or JSONL format.
func runDatasetExport(flags Flags) int {
	if flags.ExportFormat == "" {
		fmt.Fprintf(os.Stderr, "[%s] --export-format required (sharegpt, alpaca, jsonl)\n", progName)
		return 1
	}
	if flags.ExportPath == "" {
		fmt.Fprintf(os.Stderr, "[%s] --export-path required\n", progName)
		return 1
	}
	if err := dataset.ExportDataset(flags.ExportFormat, flags.ExportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Dataset export failed: %v\n", err)
		return 1
	}
	fmt.Printf("Exported dataset to %s (%s format)\n", flags.ExportPath, flags.ExportFormat)
	return 0
}

// runDatasetCurate filters and deduplicates the dataset.
func runDatasetCurate(flags Flags) int {
	minLen := flags.CurateMinLen
	maxLen := flags.CurateMaxLen
	removed, kept, err := dataset.CurateDataset(minLen, maxLen, 0.85)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Dataset curate failed: %v\n", err)
		return 1
	}
	fmt.Printf("Curated: removed %d, kept %d datapoints\n", removed, kept)
	return 0
}

// runAuto starts the autonomous agent with the given goal (--auto <goal>).
func runAuto(ctx context.Context, flags Flags, eng *llm.LocalEngine, sendMessage func(context.Context, []llm.ChatMessage, llm.StreamFunc) (string, string, error), logger *slog.Logger) int {
	fmt.Printf("%s: autonomous agent starting\n", output.Bold("Lumen Auto Mode"))
	hist := session.NewHistory("autonomous agent session")
	opts := agent.Options{
		Goal:       flags.AutoGoal,
		Sandbox:    flags.AutoSandbox,
		WorkDir:    ".",
		LiveOutput: flags.LiveOutput,
	}
	sendFunc := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return sendMessage(ctx, history, onToken)
	}
	notifyFunc := func(line string) {
		fmt.Println(output.Dim(line))
	}
	if err := agent.Run(ctx, opts, hist, sendFunc, notifyFunc); err != nil {
		fmt.Fprintf(os.Stderr, "\n%s auto agent error: %v\n", progName, err)
		return 1
	}
	return 0
}

// runEval compares a tuned model against base model on benchmark prompts.
func runEval(host string, flags Flags, logger *slog.Logger) int {
	if flags.EvalModel == "" {
		fmt.Fprintf(os.Stderr, "[%s] --eval-model required\n", progName)
		return 1
	}
	baseModel := flags.EvalBaseModel
	if baseModel == "" {
		baseModel = config.DefaultOllamaModel
	}

	prompts := []string{
		"Analyze this Go function for race conditions and nil pointer dereferences.",
		"Review this code for SQL injection vulnerabilities.",
		"Identify performance bottlenecks in this hot path function.",
		"Find memory leaks in this goroutine-heavy code.",
		"Check this authentication code for timing attacks.",
	}

	ollamaClient := ollama.NewClient(host)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("Evaluating %s vs %s on %d prompts...\n", flags.EvalModel, baseModel, len(prompts))

	for i, prompt := range prompts {
		fmt.Printf("\n--- Prompt %d/%d ---\n%s\n", i+1, len(prompts), prompt)

		for _, model := range []string{flags.EvalModel, baseModel} {
			fmt.Printf("\n[%s]:\n", model)
			resp, err := ollamaClient.Generate(ctx, ollama.GenerateRequest{
				Model:  model,
				Prompt: prompt,
				Stream: false,
				Options: ollama.Options{
					NumPredict:  200,
					Temperature: 0.1,
				},
			})
			if err != nil {
				fmt.Printf("  Error: %v\n", err)
				continue
			}
			fmt.Printf("  %s\n", truncateResponse(resp.Response, 300))
		}
	}
	return 0
}

func truncateResponse(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// runAutoLoop runs continuous generate-train-eval loop.
func runAutoLoop(host string, flags Flags, logger *slog.Logger) int {
	iterations := flags.LoopIterations
	if iterations <= 0 {
		iterations = 10
	}
	baseModel := config.DefaultOllamaModel
	tunedModel := "lumen-tuned"
	ctx := context.Background()

	fmt.Printf("Starting auto loop: %d iterations\n", iterations)

	for i := 1; i <= iterations; i++ {
		fmt.Printf("\n========== ITERATION %d/%d ==========\n", i, iterations)

		// 1. Generate
		fmt.Println("[1/3] Generating training data...")
		genOpts := dataset.GenerateOptions{
			Model:         baseModel,
			Host:          host,
			Continuous:    false,
			PipeDataset:   true,
			Topic:         "",
			TargetPath:    flags.TargetPath,
			MaxIterations: 1,
			UseHarvest:    flags.TargetPath != "",
			Logger:        logger,
		}
		if err := dataset.RunGenerate(genOpts); err != nil {
			logger.Error("generation failed", "err", err)
		}

		// 2. Train
		fmt.Println("[2/3] Training model...")
		if err := dataset.RunTrain(host, baseModel, true); err != nil {
			logger.Error("training failed", "err", err)
			continue
		}

		// 3. Evaluate
		fmt.Println("[3/3] Evaluating...")
		evalPrompts := []string{
			"Find bugs in this concurrent Go code.",
			"Identify security issues in this auth handler.",
		}
		for _, p := range evalPrompts {
			for _, m := range []string{tunedModel, baseModel} {
				resp, err := ollama.NewClient(host).Generate(ctx, ollama.GenerateRequest{
					Model:  m,
					Prompt: p,
					Stream: false,
				})
				if err != nil {
					continue
				}
				logger.Info("eval", "model", m, "prompt", p, "response_len", len(resp.Response))
			}
		}
	}

	fmt.Println("Auto loop completed.")
	return 0
}

// runAPI starts the REST API server.
func runAPI(cfg *config.Config, logger *slog.Logger) int {
	server := api.NewServer(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("shutting down API server...")
		cancel()
	}()
	defer signal.Stop(sigChan)

	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("API server error", "err", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "err", err)
		return 1
	}
	logger.Info("API server stopped")
	return 0
}

// ---------------------------------------------------------------------------
// Snapshot helpers
// ---------------------------------------------------------------------------

// createSnapshot copies targetPath (file or directory) into backupDir with
// a timestamped label ("before" or "after"). Silently returns nil if the
// path does not exist.
func createSnapshot(backupDir, targetPath, label string) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		return nil
	}
	stamp := time.Now().Format("20060102_150405")
	dest := filepath.Join(backupDir, fmt.Sprintf("snap_%s_%s", label, stamp))
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("snapshot: creating dir: %w", err)
	}
	if info.IsDir() {
		return copyDir(targetPath, filepath.Join(dest, filepath.Base(targetPath)))
	}
	return copyFile(targetPath, filepath.Join(dest, filepath.Base(targetPath)))
}

// copyFile copies a single regular file from src to dst, creating parent
// directories as needed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// copyDir recursively copies all files and directories under src into dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFile(path, target)
	})
}
