package web

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
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

func TestNeedsAuthRequiresConfiguredAuth(t *testing.T) {
	a := &WebAdapter{}
	if a.NeedsAuth(config.SourceConfig{}) {
		t.Fatal("NeedsAuth without auth = true, want false")
	}
	if !a.NeedsAuth(config.SourceConfig{Auth: "basic"}) {
		t.Fatal("NeedsAuth with auth = false, want true")
	}
}

func TestCrawlRequiresSourceURL(t *testing.T) {
	a := &WebAdapter{}
	_, pages, errs, err := a.Crawl(context.Background(), config.SourceConfig{Type: "web"}, 1)
	if err == nil {
		t.Fatal("expected missing URL error, got nil")
	}
	if pages != nil || errs != nil {
		t.Fatalf("expected nil channels on setup error, got pages=%v errs=%v", pages, errs)
	}
}

func TestCrawlFallsBackToSourceURLWhenNoLinksDiscovered(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"docs.example.com": {net.ParseIP("1.2.3.4")},
	})
	restore := replaceCrawlClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("User-Agent") != crawlUserAgent {
			t.Fatalf("User-Agent = %q, want %q", req.Header.Get("User-Agent"), crawlUserAgent)
		}
		switch req.URL.Path {
		case "/docs/sitemap.xml", "/sitemap.xml":
			return response(req, http.StatusNotFound, "text/xml", ""), nil
		case "/docs/":
			return response(req, http.StatusOK, "text/html", `<html><body><h1>Docs Home</h1><p>Welcome.</p></body></html>`), nil
		default:
			return response(req, http.StatusNotFound, "text/plain", ""), nil
		}
	})})
	defer restore()

	a := &WebAdapter{}
	total, pages, errs, err := a.Crawl(context.Background(), config.SourceConfig{
		Type: "web",
		URL:  "https://docs.example.com/docs/",
	}, 7)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}

	var got []dbPage
	for page := range pages {
		got = append(got, dbPage{
			SourceID: page.SourceID,
			URL:      page.URL,
			Title:    page.Title,
			Content:  page.Content,
			Path:     page.Path,
		})
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("crawl error: %v", err)
		}
	}
	if len(got) != 1 {
		t.Fatalf("pages = %#v, want one page", got)
	}
	if got[0].SourceID != 7 || got[0].URL != "https://docs.example.com/docs/" || got[0].Title != "Docs Home" {
		t.Fatalf("unexpected page: %#v", got[0])
	}
	if !reflect.DeepEqual(got[0].Path, []string{"docs"}) {
		t.Fatalf("page path = %#v, want [docs]", got[0].Path)
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

func TestFetchPageParsesTitleContentAndPath(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("User-Agent") != crawlUserAgent {
			t.Fatalf("User-Agent = %q, want %q", req.Header.Get("User-Agent"), crawlUserAgent)
		}
		return response(req, http.StatusOK, "text/html", `<html><body><h1>Install Guide</h1><p>Run the installer.</p></body></html>`), nil
	})}

	page, err := fetchPage(context.Background(), client, "https://docs.example.com/docs/install", 42, mustParseURL("https://docs.example.com/docs/"))
	if err != nil {
		t.Fatalf("fetchPage: %v", err)
	}
	if page.SourceID != 42 || page.URL != "https://docs.example.com/docs/install" || page.Title != "Install Guide" {
		t.Fatalf("unexpected page metadata: %#v", page)
	}
	if !strings.Contains(page.Content, "Run the installer.") {
		t.Fatalf("content = %q, want extracted body text", page.Content)
	}
	if !reflect.DeepEqual(page.Path, []string{"docs", "install"}) {
		t.Fatalf("path = %#v, want docs/install", page.Path)
	}
}

func TestFetchPageReturnsErrorForNonOKResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusNotFound, "text/plain", "missing"), nil
	})}

	_, err := fetchPage(context.Background(), client, "https://docs.example.com/missing", 1, mustParseURL("https://docs.example.com/"))
	if err == nil || !strings.Contains(err.Error(), "non-200") {
		t.Fatalf("fetchPage error = %v, want non-200 error", err)
	}
}

func TestFetchPageRetriesOnceAfterRateLimit(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			resp := response(req, http.StatusTooManyRequests, "text/plain", "slow down")
			resp.Header.Set("Retry-After", "0")
			return resp, nil
		}
		return response(req, http.StatusOK, "text/html", `<html><body><h1>Retried</h1><p>Loaded.</p></body></html>`), nil
	})}

	page, err := fetchPage(context.Background(), client, "https://docs.example.com/retry", 9, mustParseURL("https://docs.example.com/"))
	if err != nil {
		t.Fatalf("fetchPage: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if page.Title != "Retried" || page.SourceID != 9 {
		t.Fatalf("page = %#v", page)
	}
}

func TestFetchPageFallsBackToURLTitle(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return response(req, http.StatusOK, "text/html", `<html><body></body></html>`), nil
	})}

	page, err := fetchPage(context.Background(), client, "https://docs.example.com/untitled", 1, mustParseURL("https://docs.example.com/"))
	if err != nil {
		t.Fatalf("fetchPage: %v", err)
	}
	if page.Title != "https://docs.example.com/untitled" {
		t.Fatalf("Title = %q, want page URL fallback", page.Title)
	}
}

func TestDiscoverSitemapURLsUsesFirstNonEmptyCandidate(t *testing.T) {
	restore := replaceCrawlClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://docs.example.com/docs/sitemap.xml":
			return response(req, http.StatusNotFound, "text/xml", ""), nil
		case "https://docs.example.com/sitemap.xml":
			return response(req, http.StatusOK, "text/xml", `<?xml version="1.0"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://docs.example.com/docs/page</loc></url>
</urlset>`), nil
		default:
			return response(req, http.StatusNotFound, "text/plain", ""), nil
		}
	})})
	defer restore()

	got := discoverSitemapURLs(context.Background(), "https://docs.example.com/docs", mustParseURL("https://docs.example.com/docs"))
	want := []string{"https://docs.example.com/docs/page"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverSitemapURLs = %#v, want %#v", got, want)
	}
}

func TestURLToPathRootUsesHome(t *testing.T) {
	got := urlToPath(mustParseURL("https://docs.example.com/"), mustParseURL("https://docs.example.com/"))
	if !reflect.DeepEqual(got, []string{"Home"}) {
		t.Fatalf("urlToPath root = %#v, want Home", got)
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

type dbPage struct {
	SourceID int64
	URL      string
	Title    string
	Content  string
	Path     []string
}

func replaceCrawlClient(client *http.Client) func() {
	original := crawlClient
	crawlClient = client
	return func() { crawlClient = original }
}

func response(req *http.Request, status int, contentType string, body string) *http.Response {
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     header,
		Request:    req,
	}
}
