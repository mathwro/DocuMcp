package web

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type urlSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// ParseSitemap fetches and parses an XML sitemap, returning all URLs.
// If client is nil, http.DefaultClient is used.
// A 429 response is retried once after honouring the Retry-After header (capped at 60 s).
func ParseSitemap(ctx context.Context, sitemapURL string, client *http.Client) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
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
	var us urlSet
	if err := xml.NewDecoder(resp.Body).Decode(&us); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}
	urls := make([]string, len(us.URLs))
	for i, u := range us.URLs {
		urls[i] = u.Loc
	}
	return urls, nil
}
