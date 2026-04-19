package search_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/search"
)

func setupTestDB(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestFTS_ReturnsRelevantResults(t *testing.T) {
	store := setupTestDB(t)
	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	pages := []db.Page{
		{SourceID: srcID, URL: "u1", Title: "OAuth Setup", Content: "How to configure OAuth authentication and authorization flows"},
		{SourceID: srcID, URL: "u2", Title: "Docker Guide", Content: "Running containers with Docker and docker-compose"},
		{SourceID: srcID, URL: "u3", Title: "Database Schema", Content: "PostgreSQL schema design and migration strategies"},
	}
	for _, p := range pages {
		if _, err := store.UpsertPage(p); err != nil {
			t.Fatalf("UpsertPage: %v", err)
		}
	}

	results, err := search.FTS(store, "OAuth authentication", 10)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].URL != "u1" {
		t.Errorf("expected OAuth page first, got %q", results[0].URL)
	}
}

func TestFTS_ReturnsEmptyForNoMatch(t *testing.T) {
	store := setupTestDB(t)
	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "Docker Guide", Content: "Running containers"}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	results, err := search.FTS(store, "kubernetes helm charts", 10)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestFTS_RespectsLimit(t *testing.T) {
	store := setupTestDB(t)
	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := store.UpsertPage(db.Page{
			SourceID: srcID,
			URL:      fmt.Sprintf("u%d", i),
			Title:    "Authentication Guide",
			Content:  "OAuth authentication configuration",
		}); err != nil {
			t.Fatalf("UpsertPage %d: %v", i, err)
		}
	}

	results, err := search.FTS(store, "authentication", 3)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected exactly 3 results (limit enforced), got %d", len(results))
	}
}

func TestFTS_SnippetIsExcerpt(t *testing.T) {
	store := setupTestDB(t)
	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	longContent := strings.Repeat("OAuth authentication is the standard way to authorize API access. ", 50)
	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID, URL: "u1", Title: "OAuth Guide", Content: longContent,
	}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	results, err := search.FTS(store, "OAuth authentication", 10)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}

	// Snippet must be shorter than the full content — it is an excerpt, not the full page.
	if len(results[0].Snippet) >= len(longContent) {
		t.Errorf("expected snippet shorter than full content (%d chars), got %d chars",
			len(longContent), len(results[0].Snippet))
	}
	// Snippet must contain the query term.
	if !strings.Contains(strings.ToLower(results[0].Snippet), "oauth") {
		t.Errorf("expected snippet to contain 'oauth', got: %q", results[0].Snippet)
	}
}
