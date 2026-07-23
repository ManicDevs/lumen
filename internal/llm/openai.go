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

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type OpenAIEngine struct {
	host         string
	model        string
	systemPrompt string
	idleTimeout  time.Duration
	httpClient   *http.Client
	logger       *slog.Logger
	retryCfg     retry.Config
}

func NewOpenAIEngine(host, model, systemPrompt string, idleTimeout time.Duration, retryCfg retry.Config, logger *slog.Logger) *OpenAIEngine {
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	return &OpenAIEngine{
		host:         strings.TrimRight(host, "/"),
		model:        model,
		systemPrompt: systemPrompt,
		idleTimeout:  idleTimeout,
		httpClient:   &http.Client{},
		logger:       logger,
		retryCfg:     retryCfg,
	}
}

func (l *OpenAIEngine) Name() string {
	return "OpenAI-compat"
}

func (l *OpenAIEngine) Send(ctx context.Context, history []ChatMessage, onToken StreamFunc) (string, error) {
	msgs := []openAIMessage{{Role: "system", Content: l.systemPrompt}}
	for _, m := range history {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}
		msgs = append(msgs, openAIMessage{Role: role, Content: m.Content})
	}
	payload, err := json.Marshal(openAIRequest{Model: l.model, Messages: msgs, Stream: true})
	if err != nil {
		return "", retry.Permanent(fmt.Errorf("%s: marshal error: %w", l.Name(), err))
	}

	url := l.host + "/v1/chat/completions"
	var full strings.Builder

	err = retry.Do(ctx, l.retryCfg, func(ctx context.Context, attempt int) error {
		if ctx.Err() != nil {
			return retry.Permanent(ctx.Err())
		}
		full.Reset()
		if l.logger != nil {
			l.logger.Debug("openai-compat request attempt", "attempt", attempt)
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
			return fmt.Errorf("%s: network error (is server running at %s?): %w", l.Name(), l.host, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			if l.logger != nil {
				l.logger.Debug(l.Name()+" error response", "status", resp.StatusCode, "body", strings.TrimSpace(string(body)))
			}
			msg := apiErrorMessage(l.Name(), resp.StatusCode, body)
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
			if line == "" || line == "data: [DONE]" {
				continue
			}
			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(line[5:])
			}
			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			tok := chunk.Choices[0].Delta.Content
			if tok != "" {
				if onToken != nil {
					onToken(tok)
				}
				full.WriteString(tok)
			}
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return retry.Permanent(ctx.Err())
			}
			if watchdogCtx.Err() != nil {
				return fmt.Errorf("%s: stream went silent for over %s: %w", l.Name(), l.idleTimeout, watchdogCtx.Err())
			}
			return fmt.Errorf("%s: stream read error: %w", l.Name(), err)
		}
		if full.Len() == 0 {
			return errors.New(l.Name() + ": empty response")
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
