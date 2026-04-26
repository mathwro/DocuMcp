// internal/bench/tasks/config_b_test.go
package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMCP returns a JSON-RPC server that responds to tools/call with a stub text result.
func fakeMCP() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string                 `json:"method"`
			Params map[string]any         `json:"params"`
			ID     any                    `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("content-type", "application/json")
		switch req.Method {
		case "tools/call":
			name, _ := req.Params["name"].(string)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "stub-output-for-" + name},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestMCPClient_CallTool(t *testing.T) {
	srv := fakeMCP()
	defer srv.Close()

	c := NewMCPClient(srv.URL+"/mcp", "")
	out, err := c.CallTool(context.Background(), "search_docs", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(out, "stub-output-for-search_docs") {
		t.Errorf("unexpected output: %q", out)
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
