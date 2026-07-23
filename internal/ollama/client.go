package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	chatEndpoint    = "/api/chat"
	generateEndpoint = "/api/generate"
	tagsEndpoint    = "/api/tags"
	pullEndpoint    = "/api/pull"
	pushEndpoint    = "/api/push"
	deleteEndpoint  = "/api/delete"
	copyEndpoint    = "/api/copy"
	showEndpoint    = "/api/show"
	createEndpoint  = "/api/create"
	embedEndpoint   = "/api/embed"
	psEndpoint      = "/api/ps"
)

// Client is an HTTP client for the Ollama REST API. Create one with
// NewClient, then call methods like Chat, Generate, List, Pull, etc.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// ClientOption configures a Client at construction time.
type ClientOption func(*Client)

// WithHTTPClient sets the underlying http.Client (useful for custom
// timeouts, transports, or testing with httptest.Server).
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) {
		cl.httpClient = c
	}
}

// NewClient creates a Client that points at the given Ollama base URL
// (e.g. "http://localhost:11434"). Trailing slashes are trimmed.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// BaseURL returns the base URL of the Ollama server this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) doRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("ollama: marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	u, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("ollama: build URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, parseAPIError(resp.StatusCode, bodyBytes)
	}
	return resp, nil
}

func (c *Client) doStream(ctx context.Context, method, path string, body any, decode func([]byte) error) error {
	resp, err := c.doRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := decode([]byte(line)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("ollama: HTTP %d: %s", e.StatusCode, e.Message)
}

func parseAPIError(statusCode int, body []byte) error {
	var parsed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != "" {
		return &apiError{StatusCode: statusCode, Message: parsed.Error}
	}
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 200 {
		trimmed = trimmed[:200] + "..."
	}
	if trimmed == "" {
		trimmed = http.StatusText(statusCode)
	}
	return &apiError{StatusCode: statusCode, Message: trimmed}
}
