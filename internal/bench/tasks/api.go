// internal/bench/tasks/api.go
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicAPI is the production implementation of the API interface.
type AnthropicAPI struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

type APIOpt func(*AnthropicAPI)

func WithAPIBaseURL(u string) APIOpt      { return func(a *AnthropicAPI) { a.baseURL = u } }
func WithAPIClient(h *http.Client) APIOpt { return func(a *AnthropicAPI) { a.client = h } }

func NewAnthropicAPI(apiKey, model string, opts ...APIOpt) *AnthropicAPI {
	a := &AnthropicAPI{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func (a *AnthropicAPI) Send(ctx context.Context, tools []map[string]any, messages []map[string]any) (apiResponse, error) {
	system, msgs := splitSystem(messages)

	payload := map[string]any{
		"model":      a.model,
		"max_tokens": 4096,
		"messages":   msgs,
	}
	if system != "" {
		payload["system"] = system
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return apiResponse{}, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return apiResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return apiResponse{}, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiResponse{}, fmt.Errorf("read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return apiResponse{}, fmt.Errorf("messages returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var rb struct {
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBytes, &rb); err != nil {
		return apiResponse{}, fmt.Errorf("unmarshal: %w", err)
	}

	out := apiResponse{
		StopReason:   rb.StopReason,
		InputTokens:  rb.Usage.InputTokens,
		OutputTokens: rb.Usage.OutputTokens,
	}
	var textParts []string
	for _, c := range rb.Content {
		switch c.Type {
		case "text":
			textParts = append(textParts, c.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, toolCall{ID: c.ID, Name: c.Name, Input: c.Input})
		}
	}
	out.FinalText = strings.Join(textParts, "\n")
	return out, nil
}

func splitSystem(messages []map[string]any) (string, []map[string]any) {
	if len(messages) == 0 {
		return "", messages
	}
	if role, _ := messages[0]["role"].(string); role == "system" {
		sys, _ := messages[0]["content"].(string)
		return sys, messages[1:]
	}
	return "", messages
}
