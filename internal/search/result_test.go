package search_test

import (
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/search"
)

func TestFTS_SnippetMaxLength(t *testing.T) {
	store := setupTestDB(t)
	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	longContent := strings.Repeat("x", 2000)
	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID, URL: "u1", Title: "Long Page",
		Content: "authentication " + longContent,
	}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	results, err := search.FTS(store, "authentication", 10)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if len([]rune(results[0].Snippet)) > 503 { // snippetMaxChars (500) + "..."
		t.Errorf("snippet should be ≤503 chars, got %d", len([]rune(results[0].Snippet)))
	}
}

func TestTruncateSnippet_ShortPassthrough(t *testing.T) {
	input := strings.Repeat("a", 400)
	got := search.TruncateSnippet(input)
	if got != input {
		t.Errorf("expected unchanged string for short input, got len=%d", len(got))
	}
}

func TestTruncateSnippet_LongIsCapped(t *testing.T) {
	input := strings.Repeat("b", 1000)
	got := search.TruncateSnippet(input)
	if len(got) > 503 { // 500 chars + "..."
		t.Errorf("expected truncated string ≤503 chars, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected truncated string to end with '...', got: %q", got[len(got)-10:])
	}
}
