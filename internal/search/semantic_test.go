package search

import (
	"errors"
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

type fakeEmbedder struct {
	vecs [][]float32
	err  error
}

func (f fakeEmbedder) Embed(texts []string) ([][]float32, error) {
	if len(texts) != 1 || texts[0] != "oauth setup" {
		return nil, errors.New("unexpected query")
	}
	return f.vecs, f.err
}

func TestSemanticReturnsNilWhenEmbedderMissing(t *testing.T) {
	store := testutil.OpenStore(t)

	results, err := Semantic(store, nil, "oauth setup", 5)
	if err != nil {
		t.Fatalf("Semantic: %v", err)
	}
	if results != nil {
		t.Fatalf("results = %#v, want nil when embedder is missing", results)
	}
}

func TestSemanticWrapsEmbedError(t *testing.T) {
	store := testutil.OpenStore(t)
	wantErr := errors.New("embedding failed")

	_, err := Semantic(store, fakeEmbedder{err: wantErr}, "oauth setup", 5)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Semantic error = %v, want wrapped embed error", err)
	}
}

func TestSemanticReturnsNearestEmbeddingResults(t *testing.T) {
	store := testutil.OpenStore(t)
	srcID, err := store.InsertSource(db.Source{Name: "Docs", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	pageID, err := store.UpsertPage(db.Page{
		SourceID: srcID,
		URL:      "https://docs.example.com/oauth",
		Title:    "OAuth Setup",
		Content:  "<p>" + strings.Repeat("OAuth setup uses device code flow. ", 30) + "</p>",
		Path:     []string{"Auth", "OAuth"},
	})
	if err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	vec := make([]float32, 384)
	vec[0] = 1
	if err := store.UpsertEmbedding(pageID, vec); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	results, err := Semantic(store, fakeEmbedder{vecs: [][]float32{vec}}, "oauth setup", 1)
	if err != nil {
		t.Fatalf("Semantic: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].URL != "https://docs.example.com/oauth" || results[0].Title != "OAuth Setup" {
		t.Fatalf("unexpected result: %#v", results[0])
	}
	if strings.Contains(results[0].Snippet, "<p>") {
		t.Fatalf("snippet contains HTML tags: %q", results[0].Snippet)
	}
	if len(results[0].Snippet) > snippetMaxChars+3 {
		t.Fatalf("snippet length = %d, want truncated snippet", len(results[0].Snippet))
	}
}
