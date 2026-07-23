package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Options  ollamaOptions   `json:"options"`
	Stream   bool            `json:"stream"`
}

type ollamaOptions struct {
	NumCtx int `json:"num_ctx"`
}

type ollamaStreamChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

type LocalEngine struct {
	host         string
	model        string
	systemPrompt string
	numCtx       int
	idleTimeout  time.Duration
	httpClient   *http.Client
	logger       *slog.Logger
	retryCfg     retry.Config
}

func NewLocalEngine(host, model, systemPrompt string, numCtx int, idleTimeout time.Duration, retryCfg retry.Config, logger *slog.Logger) *LocalEngine {
	if numCtx <= 0 {
		numCtx = 8192
	}
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	return &LocalEngine{
		host:         strings.TrimRight(host, "/"),
		model:        model,
		systemPrompt: systemPrompt,
		numCtx:       numCtx,
		idleTimeout:  idleTimeout,
		httpClient:   &http.Client{},
		logger:       logger,
		retryCfg:     retryCfg,
	}
}

func (l *LocalEngine) Name() string {
	return "Ollama"
}

func (l *LocalEngine) Send(ctx context.Context, history []ChatMessage, onToken StreamFunc) (string, error) {
	msgs := []ollamaMessage{{Role: "system", Content: l.systemPrompt}}
	for _, m := range history {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}
		msgs = append(msgs, ollamaMessage{Role: role, Content: m.Content})
	}
	payload, err := json.Marshal(ollamaRequest{
		Model:    l.model,
		Options:  ollamaOptions{NumCtx: l.numCtx},
		Stream:   true,
		Messages: msgs,
	})
	if err != nil {
		return "", retry.Permanent(fmt.Errorf("Ollama: marshal error: %w", err))
	}

	url := l.host + "/api/chat"
	var full strings.Builder

	err = retry.Do(ctx, l.retryCfg, func(ctx context.Context, attempt int) error {
		if ctx.Err() != nil {
			return retry.Permanent(ctx.Err())
		}
		full.Reset()
		if l.logger != nil {
			l.logger.Debug("ollama request attempt", "attempt", attempt, "model", l.model)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return retry.Permanent(err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := l.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return retry.Permanent(ctx.Err())
			}
			return fmt.Errorf("Ollama: network error (is Ollama running at %s?): %w", l.host, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			if l.logger != nil {
				l.logger.Debug("Ollama error response", "status", resp.StatusCode, "body", strings.TrimSpace(string(body)))
			}
			msg := apiErrorMessage("Ollama", resp.StatusCode, body)
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				return errors.New(msg)
			}
			return retry.Permanent(errors.New(msg))
		}

		watchdogCtx, watchdogCancel := context.WithCancel(ctx)
		defer watchdogCancel()
		idle := time.AfterFunc(l.idleTimeout, watchdogCancel)
		defer idle.Stop()
		go func() {
			<-watchdogCtx.Done()
			resp.Body.Close()
		}()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
		for scanner.Scan() {
			idle.Reset(l.idleTimeout)
			if ctx.Err() != nil {
				return retry.Permanent(ctx.Err())
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var chunk ollamaStreamChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}
			if chunk.Message.Content != "" {
				if onToken != nil {
					onToken(chunk.Message.Content)
				}
				full.WriteString(chunk.Message.Content)
			}
			if chunk.Done {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return retry.Permanent(ctx.Err())
			}
			if watchdogCtx.Err() != nil {
				return fmt.Errorf("Ollama: stream went silent for over %s: %w", l.idleTimeout, watchdogCtx.Err())
			}
			return fmt.Errorf("Ollama: stream read error: %w", err)
		}
		if full.Len() == 0 {
			return errors.New("Ollama: empty response")
		}
		return nil
	})

	if err != nil {
		if full.Len() > 0 {
			return full.String(), err
		}
		return "", err
	}
	return full.String(), nil
}
