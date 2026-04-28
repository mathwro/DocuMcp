// internal/bench/tasks/config_b_test.go
package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// echoServer builds an in-process MCP server that exposes one "echo" tool
// returning the input message verbatim, wrapped in the same SSE handler
// DocuMcp uses in production. requireBearer, when non-empty, gates every
// HTTP request on a matching Authorization: Bearer header.
func echoServer(t *testing.T, requireBearer string) *httptest.Server {
	t.Helper()
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "test-echo",
		Version: "v0.0.1",
	}, nil)
	srv.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "Returns the input message.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
	}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var args struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "echo:" + args.Msg}},
		}, nil
	})

	mcpHandler := sdkmcp.NewSSEHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)

	mux := http.NewServeMux()
	// Match DocuMcp's mount point so we exercise the same path the client will hit.
	mux.Handle("/mcp/", mcpHandler)

	var handler http.Handler = mux
	if requireBearer != "" {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got != requireBearer {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			mux.ServeHTTP(w, r)
		})
	}

	return httptest.NewServer(handler)
}

func TestMCPClient_CallTool(t *testing.T) {
	srv := echoServer(t, "")
	defer srv.Close()

	c := NewMCPClient(srv.URL+"/mcp/", "")
	defer c.Close()

	out, err := c.CallTool(context.Background(), "echo", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "echo:hello" {
		t.Errorf("got %q, want %q", out, "echo:hello")
	}

	// Second call must reuse the same session — verifies lazy-connect caching.
	out2, err := c.CallTool(context.Background(), "echo", map[string]any{"msg": "again"})
	if err != nil {
		t.Fatalf("CallTool (2nd): %v", err)
	}
	if out2 != "echo:again" {
		t.Errorf("got %q, want %q", out2, "echo:again")
	}
}

func TestMCPClient_CallTool_BearerAuth(t *testing.T) {
	const token = "s3cret"
	srv := echoServer(t, token)
	defer srv.Close()

	// Wrong token: connect should fail because the SSE GET returns 401 before the
	// initialize handshake can complete.
	bad := NewMCPClient(srv.URL+"/mcp/", "wrong")
	defer bad.Close()
	if _, err := bad.CallTool(context.Background(), "echo", map[string]any{"msg": "x"}); err == nil {
		t.Errorf("expected error with wrong bearer, got nil")
	}

	// Correct token: full call must succeed end-to-end.
	good := NewMCPClient(srv.URL+"/mcp/", token)
	defer good.Close()
	out, err := good.CallTool(context.Background(), "echo", map[string]any{"msg": "ok"})
	if err != nil {
		t.Fatalf("CallTool with bearer: %v", err)
	}
	if out != "echo:ok" {
		t.Errorf("got %q, want %q", out, "echo:ok")
	}
}

func TestConfigBTools_HasFour(t *testing.T) {
	tools := ConfigBTools()
	want := map[string]bool{"list_sources": true, "search_docs": true, "browse_source": true, "get_page": true}
	if len(tools) != 4 {
		t.Fatalf("want 4 tools, got %d", len(tools))
	}
	for _, tl := range tools {
		name, _ := tl["name"].(string)
		if !want[name] {
			t.Errorf("unexpected tool: %s", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}
