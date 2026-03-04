package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
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

func (a *WebAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (<-chan db.Page, error) {
	if src.URL == "" {
		return nil, fmt.Errorf("web adapter: source URL is required")
	}

	base, err := url.Parse(src.URL)
	if err != nil {
		return nil, fmt.Errorf("web adapter: parse source URL: %w", err)
	}

	ch := make(chan db.Page, 10)

	go func() {
		defer close(ch)
		sitemapURL := strings.TrimRight(src.URL, "/") + "/sitemap.xml"
		allURLs, err := ParseSitemap(ctx, sitemapURL, crawlClient)
		if err != nil || len(allURLs) == 0 {
			// Fallback: just crawl the root URL
			allURLs = []string{src.URL}
		}

		// Filter sitemap URLs: must share the same origin as the configured source
		// URL and must not resolve to a blocked (SSRF-risk) host.
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
			if !isAllowedHost(parsed) {
				slog.Warn("web: skipping blocked host in sitemap URL", "url", u)
				continue
			}
			urls = append(urls, u)
		}
		if len(urls) == 0 {
			urls = []string{src.URL}
		}

		visited := map[string]bool{}

		for _, pageURL := range urls {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if visited[pageURL] {
				continue
			}
			visited[pageURL] = true

			page, err := fetchPage(ctx, crawlClient, pageURL, sourceID, base)
			if err != nil {
				slog.Warn("web: fetch page", "url", pageURL, "err", err)
				continue
			}
			ch <- page
		}
	}()
	return ch, nil
}

func fetchPage(ctx context.Context, client *http.Client, pageURL string, sourceID int64, base *url.URL) (db.Page, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return db.Page{}, fmt.Errorf("build request for %s: %w", pageURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return db.Page{}, fmt.Errorf("fetch %s: %w", pageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return db.Page{}, fmt.Errorf("non-200 from %s: %d", pageURL, resp.StatusCode)
	}
	title, content := ExtractText(resp.Body)
	if title == "" {
		title = pageURL
	}
	u, _ := url.Parse(pageURL)
	path := urlToPath(u, base)
	return db.Page{
		SourceID: sourceID,
		URL:      pageURL,
		Title:    title,
		Content:  content,
		Path:     path,
	}, nil
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
