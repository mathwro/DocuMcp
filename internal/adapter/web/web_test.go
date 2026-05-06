package web

import (
	"context"
	"net"
	"net/url"
	"testing"

	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/httpsafe"
)

// withLookup swaps the resolver on the shared httpsafe package for the
// duration of the test. It lives here so the web-level filterURL tests
// can still stub DNS; the deeper coverage is in internal/httpsafe.
func withLookup(t *testing.T, ips map[string][]net.IP) {
	t.Helper()
	original := httpsafe.LookupHostIPs
	httpsafe.LookupHostIPs = func(_ context.Context, host string) ([]net.IP, error) {
		if v, ok := ips[host]; ok {
			return v, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	t.Cleanup(func() { httpsafe.LookupHostIPs = original })
}

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
	withLookup(t, map[string][]net.IP{
		"docs.example.com": {net.ParseIP("1.2.3.4")},
		"other.com":        {net.ParseIP("5.6.7.8")},
	})
	base := mustParseURL("https://docs.example.com/docs/")
	filterPath := "/docs/guide/"

	cases := []struct {
		rawURL string
		want   bool
	}{
		{"https://docs.example.com/docs/guide/page1", true},
		{"https://docs.example.com/docs/guide/", true},
		{"https://docs.example.com/docs/guide", true},      // exact path without trailing slash
		{"https://docs.example.com/docs/api/page2", false}, // wrong section
		{"https://docs.example.com/docs/", false},          // parent path
		{"https://other.com/docs/guide/page1", false},      // cross-origin
	}

	for _, tc := range cases {
		u := mustParseURL(tc.rawURL)
		got := filterURL(context.Background(), u, base, filterPath)
		if got != tc.want {
			t.Errorf("filterURL(%q, base, %q) = %v, want %v", tc.rawURL, filterPath, got, tc.want)
		}
	}
}

func TestFilterURL_IncludePathsFiltersCorrectly(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"docs.example.com": {net.ParseIP("1.2.3.4")},
	})
	base := mustParseURL("https://docs.example.com/docs/")
	filterPaths := []string{"/docs/guide/", "/docs/reference/"}

	cases := []struct {
		rawURL string
		want   bool
	}{
		{"https://docs.example.com/docs/guide/page1", true},
		{"https://docs.example.com/docs/reference/page2", true},
		{"https://docs.example.com/docs/api/page3", false},
	}

	for _, tc := range cases {
		u := mustParseURL(tc.rawURL)
		got := filterURLAny(context.Background(), u, base, filterPaths)
		if got != tc.want {
			t.Errorf("filterURLAny(%q) = %v, want %v", tc.rawURL, got, tc.want)
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
