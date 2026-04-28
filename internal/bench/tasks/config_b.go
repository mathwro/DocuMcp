// internal/bench/tasks/config_b.go
package tasks

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPClient talks to a DocuMcp /mcp/ endpoint over the MCP-over-SSE transport
// using the upstream Go SDK. It connects lazily on the first CallTool and
// reuses the resulting session for subsequent calls.
type MCPClient struct {
	endpoint string
	bearer   string // optional Authorization Bearer token

	mu         sync.Mutex
	session    *sdkmcp.ClientSession
	connectErr error
	connected  bool
}

// NewMCPClient builds a client targeting the given endpoint URL. The endpoint
// must point at the MCP SSE entry path (typically `<base>/mcp` or `<base>/mcp/`).
// The bearer token is optional; when set it's added as `Authorization: Bearer <token>`
// to every HTTP request the SDK transport makes (both the SSE GET and the
// session POSTs).
func NewMCPClient(endpoint, bearer string) *MCPClient {
	return &MCPClient{
		endpoint: endpoint,
		bearer:   bearer,
	}
}

// bearerTransport injects an Authorization: Bearer header on every request.
type bearerTransport struct {
	inner http.RoundTripper
	token string
}

func (b *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone to avoid mutating the caller's request (RoundTripper contract).
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.inner.RoundTrip(r)
}

// connect dials the MCP server and runs the initialize handshake. Subsequent
// callers find a cached session (or cached connect error). Guarded by mu so
// concurrent CallTool callers serialize on the first connect.
func (c *MCPClient) connect(ctx context.Context) (*sdkmcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connected {
		return c.session, c.connectErr
	}
	c.connected = true

	httpClient := http.DefaultClient
	if c.bearer != "" {
		httpClient = &http.Client{
			Transport: &bearerTransport{
				inner: http.DefaultTransport,
				token: c.bearer,
			},
		}
	}

	transport := &sdkmcp.SSEClientTransport{
		Endpoint:   c.endpoint,
		HTTPClient: httpClient,
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "documcp-bench",
		Version: "1.0.0",
	}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		c.connectErr = fmt.Errorf("mcp connect: %w", err)
		return nil, c.connectErr
	}
	c.session = session
	return session, nil
}

// CallTool invokes the named MCP tool with the given arguments and returns the
// concatenated text content of the response. The first call establishes the
// SSE session and runs the initialize handshake; later calls reuse the session.
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	session, err := c.connect(ctx)
	if err != nil {
		return "", err
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("call tool: %w", err)
	}
	if res.IsError {
		// Surface the textual error payload, matching the LLM-visible error contract.
		return "", fmt.Errorf("mcp tool %q error: %s", name, extractText(res.Content))
	}
	return extractText(res.Content), nil
}

// Close releases the underlying SSE session if one was established. Safe to
// call zero or multiple times; bench callers are not required to call it.
func (c *MCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return nil
	}
	s := c.session
	c.session = nil
	return s.Close()
}

// extractText concatenates the text payload of every TextContent block in the
// result, ignoring image/audio/resource blocks (which the bench corpus doesn't
// expect from any of the four DocuMcp tools).
func extractText(blocks []sdkmcp.Content) string {
	var out strings.Builder
	for _, b := range blocks {
		if t, ok := b.(*sdkmcp.TextContent); ok {
			out.WriteString(t.Text)
		}
	}
	return out.String()
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
