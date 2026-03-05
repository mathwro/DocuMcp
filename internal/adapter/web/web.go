package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/documcp/documcp/internal/adapter"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

func init() {
	adapter.Register("web", &WebAdapter{})
}

// crawlClient is the HTTP client used for all web crawl requests.
// It has an explicit timeout to prevent hanging connections.
var crawlClient = &http.Client{
	Timeout: 30 * time.Second,
}

// WebAdapter crawls generic web documentation sites.
type WebAdapter struct{}

func (a *WebAdapter) NeedsAuth(src config.SourceConfig) bool {
	return src.Auth != ""
}

func (a *WebAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (int, <-chan db.Page, error) {
	if src.URL == "" {
		return 0, nil, fmt.Errorf("web adapter: source URL is required")
	}

	base, err := url.Parse(src.URL)
	if err != nil {
		return 0, nil, fmt.Errorf("web adapter: parse source URL: %w", err)
	}
	if !isAllowedHost(base) {
		return 0, nil, fmt.Errorf("web adapter: source URL %q resolves to a blocked host", src.URL)
	}

	// Discover and filter URLs before starting the goroutine so we can return
	// the total count to the caller for progress tracking.
	allURLs := discoverSitemapURLs(ctx, src.URL, base)
	basePath := strings.TrimRight(base.Path, "/") + "/"
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
		if !strings.HasPrefix(parsed.Path, basePath) && parsed.Path != strings.TrimRight(base.Path, "/") {
			continue
		}
		if !isAllowedHost(parsed) {
			slog.Warn("web: skipping blocked host in sitemap URL", "url", u)
			continue
		}
		urls = append(urls, u)
	}
	if len(urls) == 0 {
		urls = []string{src.URL}
	}

	total := len(urls)
	ch := make(chan db.Page, 10)

	go func() {
		defer close(ch)

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
				slog.Warn("web: fetch page", "url", pageURL, "err", err)
				continue
			}
			ch <- page
		}
	}()
	return total, ch, nil
}

const crawlUserAgent = "DocuMcp/1.0 (documentation indexer; +https://github.com/documcp/documcp)"

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

// isAllowedHost returns false if the URL's host is a loopback, link-local,
// or RFC-1918 private address — blocking SSRF via the crawler.
func isAllowedHost(u *url.URL) bool {
	host := u.Hostname()
	if host == "" {
		return false
	}
	// Block well-known internal hostnames.
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname — allow (full DNS resolution not feasible here).
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	for _, cidr := range privateRanges {
		if cidr.Contains(ip) {
			return false
		}
	}
	return true
}

// privateRanges lists CIDR blocks that must not be reachable via the crawler.
var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10", // Carrier-grade NAT (RFC 6598)
		"169.254.0.0/16", // IPv4 link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	ranges := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			ranges = append(ranges, network)
		}
	}
	return ranges
}()
