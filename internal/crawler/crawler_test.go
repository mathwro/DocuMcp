package crawler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
)

func TestCrawler_IndexesPages(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Use a closure to reference srv.URL after server is created
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			w.Write([]byte(`<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + srvURL + `/page1</loc></url></urlset>`))
			return
		}
		w.Write([]byte(`<html><body><h1>Test Page</h1><p>Content here.</p></body></html>`))
	}))
	defer srv.Close()
	srvURL = srv.URL

	srcID, err := store.InsertSource(db.Source{Name: "Test", Type: "web", URL: srv.URL})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	c := crawler.New(store, nil) // nil embedder = skip embeddings
	err = c.Crawl(context.Background(), db.Source{ID: srcID, Type: "web", URL: srv.URL})
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
