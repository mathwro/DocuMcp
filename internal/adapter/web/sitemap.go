package web

import (
	"encoding/xml"
	"fmt"
	"net/http"
)

type urlSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// ParseSitemap fetches and parses an XML sitemap, returning all URLs.
// If client is nil, http.DefaultClient is used.
func ParseSitemap(sitemapURL string, client *http.Client) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("fetch sitemap: %w", err)
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
