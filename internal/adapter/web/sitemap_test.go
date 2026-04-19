package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathwro/DocuMcp/internal/adapter/web"
)

func TestParseSitemap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/page1</loc></url>
  <url><loc>https://example.com/page2</loc></url>
</urlset>`))
	}))
	defer srv.Close()

	urls, err := web.ParseSitemap(context.Background(), srv.URL+"/sitemap.xml", nil)
	if err != nil {
		t.Fatalf("ParseSitemap: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(urls))
	}
}

func TestParseSitemap_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := web.ParseSitemap(context.Background(), srv.URL+"/sitemap.xml", nil)
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestParseSitemap_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></urlset>`))
	}))
	defer srv.Close()

	urls, err := web.ParseSitemap(context.Background(), srv.URL+"/sitemap.xml", nil)
	if err != nil {
		t.Fatalf("ParseSitemap: %v", err)
	}
	if len(urls) != 0 {
		t.Errorf("expected 0 URLs, got %d", len(urls))
	}
}
