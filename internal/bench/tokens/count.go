// internal/bench/tokens/count.go
package tokens

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Counter calls Anthropic's messages/count_tokens endpoint. Free, exact, no rate limit
// concerns for our scale. We prefer it over a third-party tokenizer to avoid drift.
type Counter struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

type Option func(*Counter)

func WithBaseURL(u string) Option { return func(c *Counter) { c.baseURL = u } }

func WithHTTPClient(h *http.Client) Option { return func(c *Counter) { c.client = h } }

func New(apiKey, model string, opts ...Option) *Counter {
	c := &Counter{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type requestBody struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type responseBody struct {
	InputTokens int `json:"input_tokens"`
}

// Count returns the input-token count for the given text wrapped as a single user message.
func (c *Counter) Count(ctx context.Context, text string) (int, error) {
	body, err := json.Marshal(requestBody{
		Model:    c.model,
		Messages: []message{{Role: "user", Content: text}},
	})
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages/count_tokens", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("count_tokens returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var rb responseBody
	if err := json.Unmarshal(respBytes, &rb); err != nil {
		return 0, fmt.Errorf("unmarshal: %w", err)
	}
	return rb.InputTokens, nil
}
