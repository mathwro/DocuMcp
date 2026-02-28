package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/documcp/documcp/internal/adapter"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

func init() {
	adapter.Register("web", &WebAdapter{})
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
	ch := make(chan db.Page, 10)
	client := http.DefaultClient

	go func() {
		defer close(ch)
		sitemapURL := strings.TrimRight(src.URL, "/") + "/sitemap.xml"
		urls, err := ParseSitemap(ctx, sitemapURL, client)
		if err != nil || len(urls) == 0 {
			// Fallback: just crawl the root URL
			urls = []string{src.URL}
		}

		base, err := url.Parse(src.URL)
		if err != nil {
			slog.Error("web: parse base URL", "url", src.URL, "err", err)
			return
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

			page, err := fetchPage(ctx, client, pageURL, sourceID, base)
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
