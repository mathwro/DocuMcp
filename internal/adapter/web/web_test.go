package web

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	_, _, _, err := a.Crawl(context.Background(), config.SourceConfig{
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
	_, _, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:        "web",
		URL:         "https://example.com/docs/",
		IncludePath: "https://evil.com/attack/",
	}, 1)
	if err == nil {
		t.Fatal("expected error for cross-origin include_path, got nil")
	}
}

func TestWebIncludePathFilterPaths_ResolvesRelativePaths(t *testing.T) {
	base := mustParseURL("https://docs.example.com/docs/")
	got, err := webIncludePathFilterPaths(base, []string{"guides/", "/reference/"})
	if err != nil {
		t.Fatalf("webIncludePathFilterPaths: %v", err)
	}
	want := []string{"/docs/guides/", "/reference/"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestWebIncludePathFilterPaths_PreservesFileLikePrefix(t *testing.T) {
	base := mustParseURL("https://warcraft.wiki.gg/wiki/World_of_Warcraft_API")
	got, err := webIncludePathFilterPaths(base, []string{"/wiki/API_", "/wiki/World_of_Warcraft_API"})
	if err != nil {
		t.Fatalf("webIncludePathFilterPaths: %v", err)
	}
	want := []string{"/wiki/API_", "/wiki/World_of_Warcraft_API"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
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

func TestFilterURL_IncludePathPrefixWithoutSlash(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"warcraft.wiki.gg": {net.ParseIP("1.2.3.4")},
	})
	base := mustParseURL("https://warcraft.wiki.gg/wiki/World_of_Warcraft_API")
	filterPaths := []string{"/wiki/API_", "/wiki/World_of_Warcraft_API"}

	cases := []struct {
		rawURL string
		want   bool
	}{
		{"https://warcraft.wiki.gg/wiki/World_of_Warcraft_API", true},
		{"https://warcraft.wiki.gg/wiki/World_of_Warcraft_API/Classic", true},
		{"https://warcraft.wiki.gg/wiki/API_CreateFrame", true},
		{"https://warcraft.wiki.gg/wiki/API_C_AccountInfo.GetIDFromBattleNetAccountGUID", true},
		{"https://warcraft.wiki.gg/wiki/Widget_API", false},
	}

	for _, tc := range cases {
		u := mustParseURL(tc.rawURL)
		got := filterURLAny(context.Background(), u, base, filterPaths)
		if got != tc.want {
			t.Errorf("filterURLAny(%q) = %v, want %v", tc.rawURL, got, tc.want)
		}
	}
}

func TestExtractLinksFiltersSameOriginAndPath(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"docs.example.com": {net.ParseIP("1.2.3.4")},
		"other.com":        {net.ParseIP("5.6.7.8")},
	})
	base := mustParseURL("https://docs.example.com/docs/")
	page := mustParseURL("https://docs.example.com/docs/index")
	html := strings.NewReader(`<html><body>
		<a href="/docs/guide">Guide</a>
		<a href="reference">Reference</a>
		<a href="/blog/post">Blog</a>
		<a href="https://other.com/docs/guide">Other origin</a>
		<a href="#section">Fragment only</a>
	</body></html>`)

	got := extractLinks(context.Background(), html, page, base, []string{"/docs/"})
	want := []string{
		"https://docs.example.com/docs/guide",
		"https://docs.example.com/docs/reference",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDiscoverLinkedURLsFollowsLinksWhenSitemapUnavailable(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"docs.example.com": {net.ParseIP("1.2.3.4")},
	})
	base := mustParseURL("https://docs.example.com/docs/")
	responses := map[string]string{
		"https://docs.example.com/docs/":          `<a href="/docs/guide">Guide</a>`,
		"https://docs.example.com/docs/guide":     `<a href="/docs/reference">Reference</a>`,
		"https://docs.example.com/docs/reference": `<p>Reference</p>`,
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, ok := responses[req.URL.String()]
		if !ok {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	got := discoverLinkedURLs(context.Background(), client, "https://docs.example.com/docs/", base, []string{"/docs/"})
	want := []string{
		"https://docs.example.com/docs/",
		"https://docs.example.com/docs/guide",
		"https://docs.example.com/docs/reference",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFetchPage_ReturnsBlockedErrorForCloudflareChallenge(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(strings.NewReader("challenge")),
			Header: http.Header{
				"cf-mitigated": {"challenge"},
				"Server":       {"cloudflare"},
			},
			Request: req,
		}, nil
	})}

	_, err := fetchPage(context.Background(), client, "https://docs.example.com/page", 1, mustParseURL("https://docs.example.com/"))
	if !errors.Is(err, errCrawlBlocked) {
		t.Fatalf("expected errCrawlBlocked, got %v", err)
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
