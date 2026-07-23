package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/ollama"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

type LocalEngine struct {
	client      *ollama.Client
	model       string
	systemPrompt string
	numCtx      int
	idleTimeout time.Duration
	logger      *slog.Logger
	retryCfg    retry.Config
}

func NewLocalEngine(host, model, systemPrompt string, numCtx int, idleTimeout time.Duration, retryCfg retry.Config, logger *slog.Logger) *LocalEngine {
	if numCtx <= 0 {
		numCtx = 8192
	}
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	return &LocalEngine{
		client:       ollama.NewClient(host),
		model:        model,
		systemPrompt: systemPrompt,
		numCtx:       numCtx,
		idleTimeout:  idleTimeout,
		logger:       logger,
		retryCfg:     retryCfg,
	}
}

func (l *LocalEngine) Name() string {
	return "Ollama"
}

func (l *LocalEngine) Send(ctx context.Context, history []ChatMessage, onToken StreamFunc) (string, error) {
	msgs := []ollama.Message{{Role: "system", Content: l.systemPrompt}}
	for _, m := range history {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}
		msgs = append(msgs, ollama.Message{Role: role, Content: m.Content})
	}

	req := ollama.ChatRequest{
		Model:    l.model,
		Messages: msgs,
		Options:  ollama.Options{NumCtx: l.numCtx},
	}

	// We use a mutable "partial" capture so the retry closure can reference it.
	var partial strings.Builder

	err := retry.Do(ctx, l.retryCfg, func(ctx context.Context, attempt int) error {
		if ctx.Err() != nil {
			return retry.Permanent(ctx.Err())
		}
		partial.Reset()
		if l.logger != nil {
			l.logger.Debug("ollama request attempt", "attempt", attempt, "model", l.model)
		}

		var watchdogCancel context.CancelFunc
		watchdogCtx := ctx
		if l.idleTimeout > 0 {
			watchdogCtx, watchdogCancel = context.WithCancel(ctx)
			defer watchdogCancel()
		}

		tick := time.NewTicker(l.idleTimeout)
		defer tick.Stop()

		done := make(chan struct{}, 1)
		var chatErr error

		go func() {
			defer func() { done <- struct{}{} }()

			_, chatErr = l.client.ChatStream(watchdogCtx, req, func(chunk ollama.ChatStreamChunk) error {
				if l.idleTimeout > 0 {
					tick.Reset(l.idleTimeout)
				}
				if chunk.Message.Content != "" {
					if onToken != nil {
						onToken(chunk.Message.Content)
					}
					partial.WriteString(chunk.Message.Content)
				}
				return nil
			})
		}()

		select {
		case <-done:
			if chatErr != nil {
				return fmt.Errorf("Ollama: %w", chatErr)
			}
		case <-tick.C:
			if watchdogCancel != nil {
				watchdogCancel()
			}
			<-done
			if partial.Len() > 0 {
				return fmt.Errorf("Ollama: stream went silent for over %s", l.idleTimeout)
			}
			return retry.Permanent(fmt.Errorf("Ollama: stream went silent for over %s", l.idleTimeout))
		case <-ctx.Done():
			return retry.Permanent(ctx.Err())
		}

		if partial.Len() == 0 {
			return errors.New("Ollama: empty response")
		}
		return nil
	})

	if err != nil {
		if partial.Len() > 0 {
			return partial.String(), err
		}
		return "", err
	}
	return partial.String(), nil
}
