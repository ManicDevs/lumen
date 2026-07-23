package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/agent"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/config"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/dataset"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/harvest"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/output"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

const progName = "lumen"

func Run(args []string) int {
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
		ollamaHost = "http://127.0.0.1:11434"
	}

	// --- Mode dispatch: self-contained modes that don't need an engine.
	if flags.EasterEgg {
		return runEasterEgg(ollamaHost, flags)
	}
	if flags.Train || flags.TrainAll {
		return runTrain(ollamaHost, cfg.OllamaModel, flags.TrainAll)
	}
	if flags.DatasetInit {
		return runDatasetInit()
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
		return runAuto(flags, eng, sendMessage, logger)
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
		if input == "exit" || input == "quit" {
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
			if err := agent.Run(context.Background(), opts, hist, sendFunc, notifyFunc); err != nil {
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

// --- Mode runners ---

func runEasterEgg(host string, flags Flags) int {
	if err := dataset.RunGenerate(host, config.DefaultOllamaModel, flags.Continuous, flags.PipeDataset, flags.CustomTopic); err != nil {
		fmt.Fprintf(os.Stderr, "Easter egg failed: %v\n", err)
		return 1
	}
	return 0
}

func runTrain(host, baseModel string, trainAll bool) int {
	if err := dataset.RunTrain(host, baseModel, trainAll); err != nil {
		fmt.Fprintf(os.Stderr, "Training failed: %v\n", err)
		return 1
	}
	return 0
}

func runDatasetInit() int {
	if err := dataset.RunInit(); err != nil {
		fmt.Fprintf(os.Stderr, "Dataset init failed: %v\n", err)
		return 1
	}
	return 0
}

func runAuto(flags Flags, eng *llm.LocalEngine, sendMessage func(context.Context, []llm.ChatMessage, llm.StreamFunc) (string, string, error), logger *slog.Logger) int {
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
	if err := agent.Run(context.Background(), opts, hist, sendFunc, notifyFunc); err != nil {
		fmt.Fprintf(os.Stderr, "\n%s auto agent error: %v\n", progName, err)
		return 1
	}
	return 0
}

// --- Snapshot helpers ---

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
