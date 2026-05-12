package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/adapter"
	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/httpsafe"
	"github.com/mathwro/DocuMcp/internal/sourcepaths"
	"golang.org/x/net/html"
)

func init() {
	adapter.Register("web", &WebAdapter{})
}

// crawlClient is the HTTP client used for all web crawl requests.
// It has an explicit timeout and re-validates every redirect target
// through httpsafe.CheckRedirect — without this, a public URL could
// 302 the crawler at cloud-metadata or RFC1918 addresses.
var crawlClient = &http.Client{
	Timeout:       30 * time.Second,
	CheckRedirect: httpsafe.CheckRedirect,
}

// WebAdapter crawls generic web documentation sites.
type WebAdapter struct{}

var errCrawlBlocked = errors.New("web crawl blocked by remote site")

func (a *WebAdapter) NeedsAuth(src config.SourceConfig) bool {
	return src.Auth != ""
}

func (a *WebAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (int, <-chan db.Page, <-chan error, error) {
	if src.URL == "" {
		return 0, nil, nil, fmt.Errorf("web adapter: source URL is required")
	}

	base, err := url.Parse(src.URL)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("web adapter: parse source URL: %w", err)
	}
	if !isAllowedHost(ctx, base) {
		return 0, nil, nil, fmt.Errorf("web adapter: source URL %q resolves to a blocked host", src.URL)
	}

	// Determine the path prefix to use for URL filtering.
	// If include_path is set, validate it shares the same origin and use its path.
	// Otherwise fall back to the source URL's own path.
	filterPaths := []string{strings.TrimRight(base.Path, "/") + "/"}
	includePaths := sourcepaths.Normalize(src.IncludePath, src.IncludePaths)
	if len(includePaths) > 0 {
		filterPaths, err = webIncludePathFilterPaths(base, includePaths)
		if err != nil {
			return 0, nil, nil, err
		}
	}

	// Discover and filter URLs before starting the goroutine so we can return
	// the total count to the caller for progress tracking.
	allURLs := discoverSitemapURLs(ctx, src.URL, base)
	urls := make([]string, 0, len(allURLs))
	for _, u := range allURLs {
		parsed, parseErr := url.Parse(u)
		if parseErr != nil {
			continue
		}
		if !sameOrigin(parsed, base) {
			slog.Warn("web: skipping cross-origin sitemap URL", "url", u, "base", src.URL)
			continue
		}
		if !filterURLAny(ctx, parsed, base, filterPaths) {
			if !isAllowedHost(ctx, parsed) {
				slog.Warn("web: skipping blocked host in sitemap URL", "url", u)
			}
			continue
		}
		urls = append(urls, u)
	}
	if len(urls) == 0 {
		urls = discoverLinkedURLs(ctx, crawlClient, src.URL, base, filterPaths)
	}
	if len(urls) == 0 {
		urls = []string{src.URL}
	}

	total := len(urls)
	ch := make(chan db.Page, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(ch)
		defer close(errCh)

		visited := map[string]bool{}

		for i, pageURL := range urls {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if visited[pageURL] {
				continue
			}
			visited[pageURL] = true

			// Polite crawl delay after the first page to avoid rate-limiting.
			if i > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
			}

			page, err := fetchPage(ctx, crawlClient, pageURL, sourceID, base)
			if err != nil {
				if errors.Is(err, errCrawlBlocked) {
					errCh <- err
					slog.Error("web: crawl stopped because remote site blocked automated access", "url", pageURL, "err", err)
					return
				}
				slog.Warn("web: fetch page", "url", pageURL, "err", err)
				continue
			}
			ch <- page
		}
	}()
	return total, ch, errCh, nil
}

const crawlUserAgent = "DocuMcp/1.0 (documentation indexer; +https://github.com/mathwro/DocuMcp)"

func fetchPage(ctx context.Context, client *http.Client, pageURL string, sourceID int64, base *url.URL) (db.Page, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return db.Page{}, fmt.Errorf("build request for %s: %w", pageURL, err)
	}
	req.Header.Set("User-Agent", crawlUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return db.Page{}, fmt.Errorf("fetch %s: %w", pageURL, err)
	}
	defer resp.Body.Close()
	if isBlockedResponse(resp) {
		return db.Page{}, fmt.Errorf("%w: %s returned %d (%s)", errCrawlBlocked, pageURL, resp.StatusCode, blockReason(resp))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// Honour Retry-After if present (seconds or HTTP-date).
		delay := 10 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
				delay = time.Duration(secs) * time.Second
			}
		}
		// Cap at 60 s to avoid stalling the crawl indefinitely.
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
		slog.Warn("web: rate limited, backing off", "url", pageURL, "delay", delay)
		select {
		case <-ctx.Done():
			return db.Page{}, ctx.Err()
		case <-time.After(delay):
		}
		// Retry once after the back-off.
		req2, _ := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
		req2.Header.Set("User-Agent", crawlUserAgent)
		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return db.Page{}, fmt.Errorf("fetch %s (retry): %w", pageURL, err2)
		}
		defer resp2.Body.Close()
		if isBlockedResponse(resp2) {
			return db.Page{}, fmt.Errorf("%w: %s returned %d after retry (%s)", errCrawlBlocked, pageURL, resp2.StatusCode, blockReason(resp2))
		}
		if resp2.StatusCode != http.StatusOK {
			return db.Page{}, fmt.Errorf("non-200 from %s (retry): %d", pageURL, resp2.StatusCode)
		}
		title, content := ExtractText(resp2.Body)
		if title == "" {
			title = pageURL
		}
		u, _ := url.Parse(pageURL)
		return db.Page{SourceID: sourceID, URL: pageURL, Title: title, Content: content, Path: urlToPath(u, base)}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return db.Page{}, fmt.Errorf("non-200 from %s: %d", pageURL, resp.StatusCode)
	}
	title, content := ExtractText(resp.Body)
	if title == "" {
		title = pageURL
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return db.Page{}, fmt.Errorf("parse page URL %s: %w", pageURL, err)
	}
	path := urlToPath(u, base)
	return db.Page{
		SourceID: sourceID,
		URL:      pageURL,
		Title:    title,
		Content:  content,
		Path:     path,
	}, nil
}

func isBlockedResponse(resp *http.Response) bool {
	if strings.EqualFold(resp.Header.Get("cf-mitigated"), "challenge") {
		return true
	}
	return resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(resp.Header.Get("Server")), "cloudflare")
}

func blockReason(resp *http.Response) string {
	if strings.EqualFold(resp.Header.Get("cf-mitigated"), "challenge") {
		return "Cloudflare challenge"
	}
	return "blocked response"
}

// discoverSitemapURLs tries to find a sitemap for the given source URL.
// It attempts (1) <src>/sitemap.xml and (2) <origin>/sitemap.xml, returning
// the first non-empty result. Returns nil if neither is found.
func discoverSitemapURLs(ctx context.Context, srcURL string, base *url.URL) []string {
	candidates := []string{
		strings.TrimRight(srcURL, "/") + "/sitemap.xml",
		base.Scheme + "://" + base.Host + "/sitemap.xml",
	}
	// Deduplicate (e.g. when source URL is already at the root).
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		urls, err := ParseSitemap(ctx, candidate, crawlClient)
		if err == nil && len(urls) > 0 {
			slog.Info("web: sitemap found", "url", candidate, "count", len(urls))
			return urls
		}
	}
	return nil
}

const maxLinkDiscoveredURLs = 1000

func discoverLinkedURLs(ctx context.Context, client *http.Client, srcURL string, base *url.URL, filterPaths []string) []string {
	start, err := url.Parse(srcURL)
	if err != nil {
		return nil
	}
	if !filterURLAny(ctx, start, base, filterPaths) {
		return nil
	}
	queue := []string{start.String()}
	seen := map[string]bool{start.String(): true}
	out := []string{start.String()}

	for len(queue) > 0 && len(out) < maxLinkDiscoveredURLs {
		pageURL := queue[0]
		queue = queue[1:]
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", crawlUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		page, err := url.Parse(pageURL)
		if err != nil {
			resp.Body.Close()
			continue
		}
		links := extractLinks(ctx, resp.Body, page, base, filterPaths)
		resp.Body.Close()
		for _, link := range links {
			if seen[link] {
				continue
			}
			seen[link] = true
			out = append(out, link)
			queue = append(queue, link)
			if len(out) >= maxLinkDiscoveredURLs {
				break
			}
		}
	}
	return out
}

func extractLinks(ctx context.Context, r io.Reader, page, base *url.URL, filterPaths []string) []string {
	doc, err := html.Parse(r)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var links []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
			for _, attr := range n.Attr {
				if strings.EqualFold(attr.Key, "href") {
					if link := normalizeLink(ctx, attr.Val, page, base, filterPaths); link != "" && !seen[link] {
						seen[link] = true
						links = append(links, link)
					}
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links
}

func normalizeLink(ctx context.Context, href string, page, base *url.URL, filterPaths []string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := page.ResolveReference(u)
	resolved.Fragment = ""
	resolved.RawQuery = ""
	if !filterURLAny(ctx, resolved, base, filterPaths) {
		return ""
	}
	return resolved.String()
}

func urlToPath(u, base *url.URL) []string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return []string{"Home"}
	}
	return parts
}

// sameOrigin returns true if u has the same scheme and host as base.
func sameOrigin(u, base *url.URL) bool {
	return u.Scheme == base.Scheme && strings.EqualFold(u.Host, base.Host)
}

// filterURL returns true if u should be included in a crawl whose filter
// path is filterPath and base origin is base. It checks origin, path prefix,
// and SSRF safety.
func filterURL(ctx context.Context, u *url.URL, base *url.URL, filterPath string) bool {
	if !sameOrigin(u, base) {
		return false
	}
	if !strings.HasPrefix(u.Path, filterPath) && u.Path != strings.TrimRight(filterPath, "/") {
		return false
	}
	if !isAllowedHost(ctx, u) {
		return false
	}
	return true
}

func filterURLAny(ctx context.Context, u *url.URL, base *url.URL, filterPaths []string) bool {
	for _, filterPath := range filterPaths {
		if filterURL(ctx, u, base, filterPath) {
			return true
		}
	}
	return false
}

func webIncludePathFilterPaths(base *url.URL, includePaths []string) ([]string, error) {
	filterPaths := make([]string, 0, len(includePaths))
	for _, includePath := range includePaths {
		includeParsed, parseErr := url.Parse(includePath)
		if parseErr != nil {
			return nil, fmt.Errorf("web adapter: parse include_path: %w", parseErr)
		}
		if includeParsed.IsAbs() {
			if !sameOrigin(includeParsed, base) {
				return nil, fmt.Errorf("web adapter: include_path %q must share origin with source URL %q", includePath, base.String())
			}
		} else if includeParsed.Host != "" {
			return nil, fmt.Errorf("web adapter: include_path %q must be a relative path or share origin with source URL %q", includePath, base.String())
		} else {
			includeParsed = base.ResolveReference(includeParsed)
		}
		path := includeParsed.Path
		if strings.HasSuffix(includePath, "/") {
			path = strings.TrimRight(path, "/") + "/"
		}
		filterPaths = append(filterPaths, path)
	}
	return filterPaths, nil
}

// isAllowedHost delegates to httpsafe.AllowedHost.
func isAllowedHost(ctx context.Context, u *url.URL) bool {
	return httpsafe.AllowedHost(ctx, u)
}
