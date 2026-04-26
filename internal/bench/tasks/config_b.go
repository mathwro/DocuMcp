// internal/bench/tasks/config_b.go
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// MCPClient is a minimal JSON-RPC client for DocuMcp's /mcp/ endpoint.
// We don't use the full MCP Go SDK here — we only need tools/call, and the
// minimal client keeps the bench tool's blast radius small and easy to test.
type MCPClient struct {
	endpoint string
	bearer   string // optional Authorization Bearer token
	client   *http.Client
	idCount  atomic.Int64
}

func NewMCPClient(endpoint, bearer string) *MCPClient {
	return &MCPClient{
		endpoint: endpoint,
		bearer:   bearer,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

// CallTool invokes the named MCP tool with the given arguments and returns the
// concatenated text content of the response.
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	id := c.idCount.Add(1)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	if c.bearer != "" {
		req.Header.Set("authorization", "Bearer "+c.bearer)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("mcp returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var rb struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &rb); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	if rb.Error != nil {
		return "", fmt.Errorf("mcp error: %s", rb.Error.Message)
	}
	var out strings.Builder
	for _, p := range rb.Result.Content {
		if p.Type == "text" {
			out.WriteString(p.Text)
		}
	}
	return out.String(), nil
}

// ConfigBTools returns the tool list for Configuration B: the four DocuMcp MCP tools
// declared as Anthropic function tools. Tool descriptions are intentionally brief —
// the agent learns by calling them.
func ConfigBTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_sources",
			"description": "Lists all documentation sources available in DocuMcp.",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "search_docs",
			"description": "Hybrid keyword + semantic search across indexed documentation. Returns ranked excerpts. Optional `source` filters to one source name.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":  map[string]any{"type": "string"},
					"source": map[string]any{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "browse_source",
			"description": "Hierarchical TOC for a source. Without `section`: returns top-level sections. With `section`: returns pages in that section.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source":  map[string]any{"type": "string"},
					"section": map[string]any{"type": "string"},
				},
				"required": []string{"source"},
			},
		},
		{
			"name":        "get_page",
			"description": "Returns the full extracted text of the page at the given URL.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{"type": "string"},
				},
				"required": []string{"url"},
			},
		},
	}
}
