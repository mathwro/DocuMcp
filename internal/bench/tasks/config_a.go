// internal/bench/tasks/config_a.go
package tasks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
)

// FetchURL is the handler for Configuration A's `fetch_url` function tool.
// Returns stripped text, truncated to maxChars with a "...[truncated]" marker.
func FetchURL(ctx context.Context, client *http.Client, url string, maxChars int) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "DocuMcp-Bench/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	stripped, err := pagediff.Strip(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("strip: %w", err)
	}
	if len(stripped) > maxChars {
		return stripped[:maxChars] + "...[truncated]", nil
	}
	return stripped, nil
}

// ConfigATools returns the tool list for Configuration A: web_search (server-side)
// + fetch_url (function tool). Returned as a generic []map so callers can pass it
// to whichever Anthropic SDK shape they're using.
func ConfigATools() []map[string]any {
	return []map[string]any{
		{
			"type": "web_search_20250305",
			"name": "web_search",
		},
		{
			"name":        "fetch_url",
			"description": "Fetches the given URL and returns its visible text content. Use this after web_search to read a candidate page.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Absolute URL to fetch.",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}
