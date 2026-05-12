package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mathwro/DocuMcp/internal/adapter"
	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/httpsafe"
)

func init() {
	adapter.Register("github_wiki", NewAdapter("https://api.github.com"))
}

// GitHubAdapter crawls GitHub Wiki pages via the GitHub git trees API.
type GitHubAdapter struct{ baseURL string }

// NewAdapter creates a new GitHubAdapter with the given base URL.
// The baseURL parameter enables test injection of a mock server.
func NewAdapter(baseURL string) *GitHubAdapter {
	return &GitHubAdapter{baseURL: baseURL}
}

// NeedsAuth always returns true — GitHub Wiki crawling requires a token for
// private repos. Public repos work without one but we still surface auth.
func (a *GitHubAdapter) NeedsAuth(src config.SourceConfig) bool { return true }

// Crawl fetches all Markdown pages from the given GitHub Wiki repository and
// sends them to the returned channel. The channel is closed when crawling is
// complete or ctx is cancelled.
func (a *GitHubAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (int, <-chan db.Page, <-chan error, error) {
	ch := make(chan db.Page, 10)
	errCh := make(chan error)
	go func() {
		defer close(ch)
		defer close(errCh)

		// src.Token is populated by the crawler from the token store.
		token := src.Token
		client := &http.Client{CheckRedirect: httpsafe.CheckRedirect}

		// Fetch the full git tree for the wiki (recursive=1 flattens the tree).
		treeURL := fmt.Sprintf("%s/repos/%s/git/trees/master?recursive=1", a.baseURL, src.Repo)
		req, err := http.NewRequestWithContext(ctx, "GET", treeURL, nil)
		if err != nil {
			slog.Error("github: build tree request", "err", err)
			return
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("github: fetch tree", "err", err)
			return
		}
		defer resp.Body.Close()

		var tree struct {
			Tree []struct {
				Path string `json:"path"`
				URL  string `json:"url"`
				Type string `json:"type"`
			} `json:"tree"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
			slog.Error("github: decode tree", "err", err)
			return
		}

		for _, item := range tree.Tree {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Only process Markdown blobs; skip directories, images, etc.
			if item.Type != "blob" || !strings.HasSuffix(item.Path, ".md") {
				continue
			}

			content, err := fetchBlobContent(ctx, client, item.URL, token)
			if err != nil {
				slog.Warn("github: fetch blob", "path", item.Path, "err", err)
				continue
			}

			title := fileToTitle(item.Path)
			ch <- db.Page{
				SourceID: sourceID,
				URL:      fmt.Sprintf("https://github.com/%s/wiki/%s", src.Repo, strings.TrimSuffix(item.Path, ".md")),
				Title:    title,
				Content:  content,
				Path:     []string{"Wiki", title},
			}
		}
	}()
	return 0, ch, errCh, nil
}

// fetchBlobContent retrieves and decodes the content of a single GitHub blob.
func fetchBlobContent(ctx context.Context, client *http.Client, blobURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return "", fmt.Errorf("build blob request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch blob: %w", err)
	}
	defer resp.Body.Close()

	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&blob); err != nil {
		return "", fmt.Errorf("decode blob: %w", err)
	}

	if blob.Encoding == "base64" {
		// GitHub's API includes newlines in the base64 content; strip them before decoding.
		clean := strings.ReplaceAll(blob.Content, "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			return "", fmt.Errorf("decode base64: %w", err)
		}
		return string(decoded), nil
	}
	return blob.Content, nil
}

// fileToTitle converts a wiki filename to a human-readable title.
// E.g. "Authentication-Guide.md" -> "Authentication Guide"
func fileToTitle(path string) string {
	name := strings.TrimSuffix(path, ".md")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return name
}
