package web

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ParseSitemap fetches and parses an XML sitemap, returning all URLs.
// If client is nil, http.DefaultClient is used.
// A 429 response is retried once after honouring the Retry-After header (capped at 60 s).
func ParseSitemap(ctx context.Context, sitemapURL string, client *http.Client) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return parseSitemap(ctx, sitemapURL, client, 0)
}

func parseSitemap(ctx context.Context, sitemapURL string, client *http.Client, depth int) ([]string, error) {
	if depth > 3 {
		return nil, fmt.Errorf("sitemap index nesting too deep")
	}
	doRequest := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", sitemapURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build sitemap request: %w", err)
		}
		req.Header.Set("User-Agent", crawlUserAgent)
		return client.Do(req)
	}

	resp, err := doRequest()
	if err != nil {
		return nil, fmt.Errorf("fetch sitemap: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		delay := 10 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, parseErr := strconv.Atoi(ra); parseErr == nil {
				delay = time.Duration(secs) * time.Second
			}
		}
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		resp, err = doRequest()
		if err != nil {
			return nil, fmt.Errorf("fetch sitemap (retry): %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sitemap returned %d", resp.StatusCode)
	}
	var doc struct {
		XMLName xml.Name `xml:""`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}
	if len(doc.Sitemaps) > 0 {
		return parseSitemapIndex(ctx, sitemapURL, client, doc.Sitemaps, depth)
	}
	urls := make([]string, len(doc.URLs))
	for i, u := range doc.URLs {
		urls[i] = u.Loc
	}
	return urls, nil
}

func parseSitemapIndex(ctx context.Context, sitemapURL string, client *http.Client, sitemaps []struct {
	Loc string `xml:"loc"`
}, depth int) ([]string, error) {
	base, err := url.Parse(sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("parse sitemap URL: %w", err)
	}
	urls := []string{}
	for _, sm := range sitemaps {
		child, err := url.Parse(sm.Loc)
		if err != nil {
			continue
		}
		if child.Scheme != base.Scheme || child.Host != base.Host {
			continue
		}
		childURLs, err := parseSitemap(ctx, sm.Loc, client, depth+1)
		if err != nil {
			continue
		}
		urls = append(urls, childURLs...)
	}
	return urls, nil
}
