package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/autoagent"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/config"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/engine"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/harvest"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

const progName = "lumen"

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage (Code Mode): %s <target_path_or_module> [--auto-sandbox]\n", progName)
		fmt.Fprintf(os.Stderr, "Usage (Chat Mode): %s --chat [--auto-sandbox] [--easter-egg] [--continuous] [--pipe-dataset] [--topic \"custom topic\"]\n", progName)
		fmt.Fprintf(os.Stderr, "Usage (Train Mode): %s --train | %s --train-all\n", progName, progName)
		fmt.Fprintf(os.Stderr, "Usage (Dataset Mode): %s --dataset-init\n", progName)
		return 1
	}

	targetMode := "code"
	if os.Args[1] == "--chat" || os.Args[1] == "--easter-egg" || os.Args[1] == "--train" || os.Args[1] == "--train-all" || os.Args[1] == "--dataset-init" {
		targetMode = "chat"
	}

	autoSandbox := false
	easterEgg := false
	continuous := false
	pipeDataset := false
	train := false
	trainAll := false
	datasetInit := false
	customTopic := ""
	targetPath := ""

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--easter-egg":
			easterEgg = true
			targetMode = "chat"
		case "--train":
			train = true
			targetMode = "chat"
		case "--train-all":
			train = true
			trainAll = true
			targetMode = "chat"
		case "--dataset-init":
			datasetInit = true
			targetMode = "chat"
		case "--auto-sandbox":
			autoSandbox = true
		case "--continuous", "--autonomous":
			continuous = true
		case "--pipe-dataset":
			pipeDataset = true
		case "--chat":
			// mode flag only, no path attached
		case "--topic":
			if i+1 < len(args) {
				customTopic = args[i+1]
				i++
			}
		default:
			// First bare (non "--flag") argument in code mode is the
			// harvest target; everything else is ignored rather than
			// erroring, to stay forgiving of flag ordering.
			if targetMode == "code" && targetPath == "" && !strings.HasPrefix(args[i], "--") {
				targetPath = args[i]
			}
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] config error: %v\n", progName, err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] config invalid: %v\n", progName, err)
		return 1
	}

	ollamaHost := cfg.OllamaHost
	if ollamaHost == "" {
		ollamaHost = "http://127.0.0.1:11434"
	}

	if easterEgg {
		if err := engine.RunBunnyEasterEgg(config.DefaultOllamaModel, ollamaHost, continuous, pipeDataset, customTopic); err != nil {
			fmt.Fprintf(os.Stderr, "Easter egg failed: %v\n", err)
			return 1
		}
		return 0
	}

	if train {
		if err := engine.RunLocalTrain(ollamaHost, config.DefaultOllamaModel, trainAll); err != nil {
			fmt.Fprintf(os.Stderr, "Training failed: %v\n", err)
			return 1
		}
		return 0
	}

	if datasetInit {
		if err := engine.RunDatasetInit(); err != nil {
			fmt.Fprintf(os.Stderr, "Dataset init failed: %v\n", err)
			return 1
		}
		return 0
	}

	if targetMode == "code" && targetPath == "" {
		fmt.Fprintf(os.Stderr, "[%s] Code Mode requires a target file or directory path\n", progName)
		return 1
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

	// --- Engine: local Ollama only. No cloud fallback, no API keys, no
	// per-token billing — everything stays on this machine.
	localEngine := engine.NewLocalEngine(
		engine.BackendOllama, ollamaHost, cfg.OllamaModel, cfg.SystemPrompt,
		cfg.OllamaNumCtx, cfg.RequestTimeout, retryCfg, logger,
	)

	sendMessage := func(ctx context.Context, history []engine.ChatMessage, onToken engine.StreamFunc) (string, string, error) {
		reply, err := localEngine.Send(ctx, history, onToken)
		return localEngine.Name(), reply, err
	}

	// --- Seed the session: harvested code in Code Mode, a plain opener in
	// Chat Mode.
	initialContext := "chat context"
	if targetMode == "code" {
		if err := harvest.ValidateTargetPath(targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "[%s] %v\n", progName, err)
			return 1
		}
		if err := harvest.CreateSnapshot(cfg.BackupDir, targetPath, "before"); err != nil {
			logger.Warn("could not snapshot target before session", "err", err)
		}
		ctxBlock, err := harvest.Context(targetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] harvest error: %v\n", progName, err)
			return 1
		}
		if strings.TrimSpace(ctxBlock) == "" {
			fmt.Fprintf(os.Stderr, "[%s] no recognized source files found under %q\n", progName, targetPath)
			return 1
		}
		initialContext = ctxBlock
		fmt.Printf("Lumen Code Mode: harvested %s\n", targetPath)
	} else {
		fmt.Println("Lumen Chat Shell Initialized.")
	}

	hist := session.New(initialContext)

	runExchange := func() {
		start := time.Now()
		name, reply, err := sendMessage(context.Background(), hist.Snapshot(), func(tok string) {
			fmt.Print(tok)
		})
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
		hist.Append(engine.ChatMessage{Role: "assistant", Content: reply})
		if auditLog != nil {
			auditLog.Write(session.AuditEntry{
				Role: "assistant", TokenCount: session.ApproxTokens(reply),
				DurationMS: dur.Milliseconds(), Engine: name,
			})
		}
	}

	// Code Mode fires one review pass immediately off the harvested
	// context, then drops into the same interactive shell as Chat Mode
	// for follow-up questions.
	if targetMode == "code" {
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
			opts := autoagent.Options{
				Goal:    goal,
				Sandbox: autoSandbox,
				WorkDir: ".",
			}
			sendFunc := func(ctx context.Context, history []engine.ChatMessage, onToken engine.StreamFunc) (string, string, error) {
				return sendMessage(ctx, history, onToken)
			}
			notifyFunc := func(line string) {
				fmt.Println(line)
			}

			if err := autoagent.Run(context.Background(), opts, hist, sendFunc, notifyFunc); err != nil {
				fmt.Printf("auto agent error: %v\n", err)
			}
			continue
		}

		hist.Append(engine.ChatMessage{Role: "user", Content: input})
		if auditLog != nil {
			auditLog.Write(session.AuditEntry{Role: "user", TokenCount: session.ApproxTokens(input)})
		}
		fmt.Print("\n[Lumen]: ")
		runExchange()
	}

	if targetMode == "code" {
		if err := harvest.CreateSnapshot(cfg.BackupDir, targetPath, "after"); err != nil {
			logger.Warn("could not snapshot target after session", "err", err)
		}
	}

	return 0
}
