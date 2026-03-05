package web

import (
	"context"
	"net/url"
	"testing"

	"github.com/documcp/documcp/internal/config"
)

func TestCrawl_IncludePath_InvalidURL(t *testing.T) {
	a := &WebAdapter{}
	_, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:        "web",
		URL:         "https://example.com/docs/",
		IncludePath: "://bad-url",
	}, 1)
	if err == nil {
		t.Fatal("expected error for invalid include_path, got nil")
	}
}

func TestCrawl_IncludePath_CrossOrigin(t *testing.T) {
	a := &WebAdapter{}
	_, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:        "web",
		URL:         "https://example.com/docs/",
		IncludePath: "https://evil.com/attack/",
	}, 1)
	if err == nil {
		t.Fatal("expected error for cross-origin include_path, got nil")
	}
}

func TestFilterURL_IncludePathFiltersCorrectly(t *testing.T) {
	base := mustParseURL("https://docs.example.com/docs/")
	filterPath := "/docs/guide/"

	cases := []struct {
		rawURL string
		want   bool
	}{
		{"https://docs.example.com/docs/guide/page1", true},
		{"https://docs.example.com/docs/guide/", true},
		{"https://docs.example.com/docs/guide", true}, // exact path without trailing slash
		{"https://docs.example.com/docs/api/page2", false},  // wrong section
		{"https://docs.example.com/docs/", false},           // parent path
		{"https://other.com/docs/guide/page1", false},       // cross-origin
	}

	for _, tc := range cases {
		u := mustParseURL(tc.rawURL)
		got := filterURL(u, base, filterPath)
		if got != tc.want {
			t.Errorf("filterURL(%q, base, %q) = %v, want %v", tc.rawURL, filterPath, got, tc.want)
		}
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
