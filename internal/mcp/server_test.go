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

	_, err := store.InsertSource(db.Source{
		Name: "Test Docs",
		Type: "web",
		URL:  "https://example.com",
		Auth: "super-secret-token",
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
		t.Errorf("list_sources response does not contain source name; got: %s", text)
	}

	// Auth field must not appear in the response — it holds encrypted tokens.
	if strings.Contains(text, "super-secret-token") {
		t.Error("list_sources response must not contain auth token value")
	}
	if strings.Contains(text, `"auth"`) || strings.Contains(text, `"Auth"`) {
		t.Error("list_sources response must not contain 'auth' field at all")
	}

	// Verify the JSON is valid and the source name is present.
	var sources []map[string]any
	if err := json.Unmarshal([]byte(text), &sources); err != nil {
		t.Fatalf("unmarshal list_sources response: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources))
	}
	if sources[0]["name"] != "Test Docs" {
		t.Errorf("expected source name 'Test Docs', got %v", sources[0]["name"])
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

func TestSearchDocs_ResultDTO(t *testing.T) {
	store := openTestDB(t)

	srcID, err := store.InsertSource(db.Source{
		Name: "Go Docs",
		Type: "web",
		URL:  "https://pkg.go.dev",
	})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID,
		URL:      "https://pkg.go.dev/context",
		Title:    "context package",
		Content:  "Package context defines the Context type which carries deadlines and cancellation signals.",
		Path:     []string{"stdlib", "context"},
	}); err != nil {
		t.Fatalf("upsert page: %v", err)
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "search_docs",
		Arguments: map[string]any{"query": "context cancellation"},
	})
	if err != nil {
		t.Fatalf("search_docs call: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_docs returned error: %v", res.Content)
	}

	text := res.Content[0].(*sdkmcp.TextContent).Text

	var results []map[string]any
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	r := results[0]

	// Source name must be present and correct.
	if r["sourceName"] != "Go Docs" {
		t.Errorf("expected sourceName 'Go Docs', got %v", r["sourceName"])
	}

	// Raw SourceID and Score must not appear — they are internal implementation details.
	for _, forbidden := range []string{"SourceID", "sourceId", "Score", "score"} {
		if _, ok := r[forbidden]; ok {
			t.Errorf("search result must not contain field %q", forbidden)
		}
	}

	// Core fields must be present.
	if r["url"] == nil {
		t.Error("expected 'url' field in result")
	}
	if r["snippet"] == nil {
		t.Error("expected 'snippet' field in result")
	}
}

