package mcp_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/documcp/documcp/internal/db"
	mcpserver "github.com/documcp/documcp/internal/mcp"
)

// openTestDB creates a temporary SQLite database for testing.
func openTestDB(t *testing.T) *db.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// connectTestClient connects an in-memory MCP client to the given server and
// returns a ClientSession. The session is closed when the test ends.
func connectTestClient(t *testing.T, s *mcpserver.Server) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	t1, t2 := sdkmcp.NewInMemoryTransports()
	if _, err := s.SDKServer().Connect(ctx, t1, nil); err != nil {
		t.Fatalf("connect server transport: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestListSources(t *testing.T) {
	store := openTestDB(t)

	// Insert a test source.
	_, err := store.InsertSource(db.Source{
		Name: "Test Docs",
		Type: "web",
		URL:  "https://example.com",
	})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "list_sources",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("list_sources call: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_sources returned error: %v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("list_sources returned no content")
	}

	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "Test Docs") {
		t.Errorf("list_sources response does not contain %q; got: %s", "Test Docs", text)
	}

	// Verify the JSON is valid and contains the source name.
	var sources []db.Source
	if err := json.Unmarshal([]byte(text), &sources); err != nil {
		t.Fatalf("unmarshal list_sources response: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "Test Docs" {
		t.Errorf("expected source name %q, got %q", "Test Docs", sources[0].Name)
	}
}

func TestGetPageNotFound(t *testing.T) {
	store := openTestDB(t)
	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "get_page",
		Arguments: map[string]any{"url": "https://example.com/nonexistent"},
	})
	if err != nil {
		t.Fatalf("get_page call: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected get_page to return an error for unknown URL, but got success")
	}
}

func TestSearchDocsNoResults(t *testing.T) {
	store := openTestDB(t)
	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "search_docs",
		Arguments: map[string]any{"query": "xyzzy nonexistent term"},
	})
	if err != nil {
		t.Fatalf("search_docs call: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_docs returned tool error: %v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("search_docs returned no content")
	}
	// Should return an empty JSON array.
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "[]") {
		t.Errorf("expected empty array, got: %s", text)
	}
}

func TestBrowseSourceNotFound(t *testing.T) {
	store := openTestDB(t)
	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "browse_source",
		Arguments: map[string]any{"source": "no-such-source"},
	})
	if err != nil {
		t.Fatalf("browse_source call: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected browse_source to return error for unknown source, but got success")
	}
}

