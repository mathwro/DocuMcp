package azuredevops

import (
	"context"
	"encoding/json"
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
	adapter.Register("azure_devops", NewAdapter(""))
}

// AzureDevOpsAdapter crawls Azure DevOps Wiki pages via the REST API.
type AzureDevOpsAdapter struct{ baseURL string }

// NewAdapter returns an AzureDevOpsAdapter. baseURL overrides the source's
// BaseURL when set (used in tests to point at a mock server).
func NewAdapter(baseURL string) *AzureDevOpsAdapter {
	return &AzureDevOpsAdapter{baseURL: baseURL}
}

// NeedsAuth always returns true — Azure DevOps requires a Bearer token.
func (a *AzureDevOpsAdapter) NeedsAuth(_ config.SourceConfig) bool { return true }

// Crawl fetches all wiki pages from Azure DevOps and streams them to the
// returned channel. The channel is closed when crawling finishes or ctx is
// cancelled.
func (a *AzureDevOpsAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (<-chan db.Page, error) {
	ch := make(chan db.Page, 10)
	go func() {
		defer close(ch)

		// src.Token is populated by the crawler from the token store.
		token := src.Token
		client := &http.Client{}

		// Determine base API URL: adapter override wins, then source BaseURL.
		apiBase := a.baseURL
		if apiBase == "" {
			apiBase = src.BaseURL
		}

		// List all pages recursively.
		pagesURL := fmt.Sprintf("%s/pages?api-version=7.1&recursionLevel=full&path=/", apiBase)
		req, err := http.NewRequestWithContext(ctx, "GET", pagesURL, nil)
		if err != nil {
			slog.Error("azuredevops: build pages request", "err", err)
			return
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("azuredevops: fetch pages list", "err", err)
			return
		}
		defer resp.Body.Close()

		var result struct {
			Value []struct {
				Path string `json:"path"`
				ID   string `json:"id"`
			} `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			slog.Error("azuredevops: decode pages list", "err", err)
			return
		}

		for _, item := range result.Value {
			select {
			case <-ctx.Done():
				return
			default:
			}

			content, err := fetchPageContent(ctx, client, apiBase, item.Path, token)
			if err != nil {
				slog.Warn("azuredevops: fetch page content", "path", item.Path, "err", err)
				continue
			}

			path := wikiPathToSlice(item.Path)
			title := ""
			if len(path) > 0 {
				title = path[len(path)-1]
			}
			if title == "" {
				title = item.Path
			}

			ch <- db.Page{
				SourceID: sourceID,
				URL:      fmt.Sprintf("%s/wiki%s", apiBase, item.Path),
				Title:    title,
				Content:  content,
				Path:     path,
			}
		}
	}()
	return ch, nil
}

// fetchPageContent retrieves the markdown content of a single wiki page.
func fetchPageContent(ctx context.Context, client *http.Client, apiBase, path, token string) (string, error) {
	pageURL := fmt.Sprintf("%s/pages?api-version=7.1&path=%s", apiBase, url.QueryEscape(path))
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("build page request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	var page struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return "", fmt.Errorf("decode page: %w", err)
	}
	return page.Content, nil
}

// wikiPathToSlice converts a wiki path like "/Architecture/Overview" into a
// string slice ["Architecture", "Overview"], replacing hyphens with spaces.
func wikiPathToSlice(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return []string{"Home"}
	}
	parts := strings.Split(trimmed, "/")
	result := make([]string, len(parts))
	for i, p := range parts {
		result[i] = strings.ReplaceAll(p, "-", " ")
	}
	return result
}
