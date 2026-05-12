package crawler_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/adapter"
	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/crawler"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

// stubAdapter is a test-only adapter that returns a fixed set of pages without
// making real HTTP requests, avoiding the web adapter's SSRF protection.
type stubAdapter struct{}

func (s *stubAdapter) NeedsAuth(_ config.SourceConfig) bool { return false }
func (s *stubAdapter) Crawl(_ context.Context, _ config.SourceConfig, sourceID int64) (int, <-chan db.Page, <-chan error, error) {
	ch := make(chan db.Page, 2)
	errCh := make(chan error)
	ch <- db.Page{SourceID: sourceID, URL: "http://example.test/page1", Title: "Test Page", Content: "Content here."}
	close(ch)
	close(errCh)
	return 1, ch, errCh, nil
}

type failingStubAdapter struct{}

func (s *failingStubAdapter) NeedsAuth(_ config.SourceConfig) bool { return false }
func (s *failingStubAdapter) Crawl(_ context.Context, _ config.SourceConfig, _ int64) (int, <-chan db.Page, <-chan error, error) {
	ch := make(chan db.Page)
	errCh := make(chan error, 1)
	close(ch)
	errCh <- errors.New("blocked")
	close(errCh)
	return 1, ch, errCh, nil
}

func init() {
	adapter.Register("stub", &stubAdapter{})
	adapter.Register("failing_stub", &failingStubAdapter{})
}

func TestCrawler_IndexesPages(t *testing.T) {
	store := testutil.OpenStore(t)

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
	store := testutil.OpenStore(t)

	c := crawler.New(store, nil)
	err := c.Crawl(context.Background(), db.Source{ID: 1, Type: "unknown_source_type"})
	if err == nil {
		t.Fatal("expected error for unknown source type, got nil")
	}
}

func TestCrawler_ReturnsTerminalAdapterError(t *testing.T) {
	store := testutil.OpenStore(t)
	c := crawler.New(store, nil)

	err := c.Crawl(context.Background(), db.Source{ID: 1, Type: "failing_stub"})
	if err == nil {
		t.Fatal("expected terminal crawl error, got nil")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got %v", err)
	}
}
