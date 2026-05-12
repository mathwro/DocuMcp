package mcp_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mathwro/DocuMcp/internal/db"
	mcpserver "github.com/mathwro/DocuMcp/internal/mcp"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

// openTestDB creates a temporary SQLite database for testing.
func openTestDB(t *testing.T) *db.Store {
	t.Helper()
	return testutil.OpenStoreFile(t)
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

func TestStreamableHTTPHandlerListsTools(t *testing.T) {
	store := openTestDB(t)
	srv := mcpserver.NewServer(store, nil)
	httpSrv := httptest.NewServer(srv.StreamableHTTPHandler())
	t.Cleanup(httpSrv.Close)

	ctx := context.Background()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: httpSrv.URL}, nil)
	if err != nil {
		t.Fatalf("connect streamable client: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	res, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := make(map[string]bool)
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, name := range []string{"list_sources", "search_docs", "browse_source", "get_page", "get_page_excerpt"} {
		if !got[name] {
			t.Fatalf("expected streamable handler to expose %q; got %#v", name, got)
		}
	}
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

func TestSearchDocs_SourceFilter(t *testing.T) {
	store := openTestDB(t)

	// Create two sources with one page each.
	src1, err := store.InsertSource(db.Source{Name: "React", Type: "web", URL: "https://react.dev"})
	if err != nil {
		t.Fatalf("insert source 1: %v", err)
	}
	src2, err := store.InsertSource(db.Source{Name: "Vue", Type: "web", URL: "https://vuejs.org"})
	if err != nil {
		t.Fatalf("insert source 2: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{
		SourceID: src1, URL: "https://react.dev/hooks",
		Title: "React Hooks", Content: "Hooks let you use state in function components.",
		Path: []string{"API", "Hooks"},
	}); err != nil {
		t.Fatalf("upsert page 1: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{
		SourceID: src2, URL: "https://vuejs.org/composables",
		Title: "Vue Composables", Content: "Composables let you use state in composition API.",
		Path: []string{"API", "Composables"},
	}); err != nil {
		t.Fatalf("upsert page 2: %v", err)
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "search_docs",
		Arguments: map[string]any{"query": "state components", "source": "React"},
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

	// All results must be from "React", not "Vue".
	for i, r := range results {
		if r["sourceName"] != "React" {
			t.Errorf("result %d: expected sourceName 'React', got %v", i, r["sourceName"])
		}
	}
}

func TestBrowseSource_Success(t *testing.T) {
	store := openTestDB(t)

	srcID, err := store.InsertSource(db.Source{Name: "Docs", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID, URL: "https://example.com/guide/intro",
		Title: "Introduction", Content: "Welcome to the docs.",
		Path: []string{"Guide", "Introduction"},
	}); err != nil {
		t.Fatalf("upsert page 1: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID, URL: "https://example.com/api/auth",
		Title: "Auth API", Content: "Authentication endpoints.",
		Path: []string{"API", "Auth"},
	}); err != nil {
		t.Fatalf("upsert page 2: %v", err)
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)
	ctx := context.Background()

	// Top-level browse: should return sections with page counts.
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "browse_source",
		Arguments: map[string]any{"source": "Docs"},
	})
	if err != nil {
		t.Fatalf("browse_source call: %v", err)
	}
	if res.IsError {
		t.Fatalf("browse_source returned error: %v", res.Content)
	}
	text := res.Content[0].(*sdkmcp.TextContent).Text
	var sections []map[string]any
	if err := json.Unmarshal([]byte(text), &sections); err != nil {
		t.Fatalf("unmarshal sections: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}

	// Drill into a section: should return pages.
	res, err = cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "browse_source",
		Arguments: map[string]any{"source": "Docs", "section": "Guide"},
	})
	if err != nil {
		t.Fatalf("browse_source section call: %v", err)
	}
	if res.IsError {
		t.Fatalf("browse_source section returned error: %v", res.Content)
	}
	text = res.Content[0].(*sdkmcp.TextContent).Text
	var pages []map[string]any
	if err := json.Unmarshal([]byte(text), &pages); err != nil {
		t.Fatalf("unmarshal pages: %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("expected 1 page in Guide section, got %d", len(pages))
	}
	if pages[0]["Title"] != "Introduction" {
		t.Errorf("expected page title 'Introduction', got %v", pages[0]["Title"])
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
	if r["resultId"] == nil {
		t.Error("expected 'resultId' field in result")
	}
}

func TestSearchDocs_DefaultLimitAndSnippetChars(t *testing.T) {
	store := openTestDB(t)

	srcID, err := store.InsertSource(db.Source{Name: "Docs", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	for i := 0; i < 7; i++ {
		if _, err := store.UpsertPage(db.Page{
			SourceID: srcID,
			URL:      "https://example.com/page-" + string(rune('a'+i)),
			Title:    "Page",
			Content:  "needle " + strings.Repeat("long content ", 80),
			Path:     []string{"Reference"},
		}); err != nil {
			t.Fatalf("upsert page %d: %v", i, err)
		}
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "search_docs",
		Arguments: map[string]any{"query": "needle"},
	})
	if err != nil {
		t.Fatalf("search_docs call: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_docs returned error: %v", res.Content)
	}

	var results []map[string]any
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected default limit of 5 results, got %d", len(results))
	}
	for _, r := range results {
		snippet, _ := r["snippet"].(string)
		if len([]rune(snippet)) > 303 {
			t.Fatalf("expected default snippet <=303 chars, got %d: %q", len([]rune(snippet)), snippet)
		}
	}
}

func TestSearchDocs_CustomLimitAndSnippetChars(t *testing.T) {
	store := openTestDB(t)

	srcID, err := store.InsertSource(db.Source{Name: "Docs", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := store.UpsertPage(db.Page{
			SourceID: srcID,
			URL:      "https://example.com/custom-" + string(rune('a'+i)),
			Title:    "Custom",
			Content:  "needle " + strings.Repeat("custom content ", 50),
			Path:     []string{"Reference"},
		}); err != nil {
			t.Fatalf("upsert page %d: %v", i, err)
		}
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "search_docs",
		Arguments: map[string]any{"query": "needle", "limit": 2, "snippet_chars": 80},
	})
	if err != nil {
		t.Fatalf("search_docs call: %v", err)
	}
	if res.IsError {
		t.Fatalf("search_docs returned error: %v", res.Content)
	}

	var results []map[string]any
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected custom limit of 2 results, got %d", len(results))
	}
	for _, r := range results {
		snippet, _ := r["snippet"].(string)
		if len([]rune(snippet)) > 83 {
			t.Fatalf("expected custom snippet <=83 chars, got %d: %q", len([]rune(snippet)), snippet)
		}
	}
}

func TestGetPageExcerpt(t *testing.T) {
	store := openTestDB(t)

	srcID, err := store.InsertSource(db.Source{Name: "Docs", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	content := "opening " + strings.Repeat("alpha ", 80) + "target phrase " + strings.Repeat("beta ", 80)
	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID,
		URL:      "https://example.com/long",
		Title:    "Long Page",
		Content:  content,
		Path:     []string{"Guide"},
	}); err != nil {
		t.Fatalf("upsert page: %v", err)
	}

	srv := mcpserver.NewServer(store, nil)
	cs := connectTestClient(t, srv)
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "get_page_excerpt",
		Arguments: map[string]any{"url": "https://example.com/long", "query": "target", "max_chars": 120},
	})
	if err != nil {
		t.Fatalf("get_page_excerpt call: %v", err)
	}
	if res.IsError {
		t.Fatalf("get_page_excerpt returned error: %v", res.Content)
	}
	text := res.Content[0].(*sdkmcp.TextContent).Text
	if !strings.Contains(text, "target phrase") {
		t.Fatalf("expected focused excerpt to include target phrase, got %q", text)
	}
	if len([]rune(text)) > 123 {
		t.Fatalf("expected bounded excerpt <=123 chars, got %d", len([]rune(text)))
	}
}
