package crawler_test

import (
	"context"
	"testing"

	"github.com/documcp/documcp/internal/adapter"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
)

// stubAdapter is a test-only adapter that returns a fixed set of pages without
// making real HTTP requests, avoiding the web adapter's SSRF protection.
type stubAdapter struct{}

func (s *stubAdapter) NeedsAuth(_ config.SourceConfig) bool { return false }
func (s *stubAdapter) Crawl(_ context.Context, _ config.SourceConfig, sourceID int64) (<-chan db.Page, error) {
	ch := make(chan db.Page, 2)
	ch <- db.Page{SourceID: sourceID, URL: "http://example.test/page1", Title: "Test Page", Content: "Content here."}
	close(ch)
	return ch, nil
}

func init() {
	adapter.Register("stub", &stubAdapter{})
}

func TestCrawler_IndexesPages(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	srcID, err := store.InsertSource(db.Source{Name: "Test", Type: "stub", URL: "http://example.test"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	c := crawler.New(store, nil) // nil embedder = skip embeddings
	err = c.Crawl(context.Background(), db.Source{ID: srcID, Type: "stub", URL: "http://example.test"})
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no sources returned")
	}
	if sources[0].PageCount == 0 {
		t.Error("expected pages to be indexed")
	}
}

func TestCrawler_UnknownType(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	c := crawler.New(store, nil)
	err = c.Crawl(context.Background(), db.Source{ID: 1, Type: "unknown_source_type"})
	if err == nil {
		t.Fatal("expected error for unknown source type, got nil")
	}
}
