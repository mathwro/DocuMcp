package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathwro/DocuMcp/internal/adapter/github"
	"github.com/mathwro/DocuMcp/internal/config"
)

func TestGitHubAdapter_CrawlsWikiPages(t *testing.T) {
	var srv *httptest.Server
	// Mock GitHub API: returns a git tree with .md files
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/myorg/myrepo/git/trees/master":
			json.NewEncoder(w).Encode(map[string]any{
				"tree": []map[string]any{
					{"path": "Home.md", "type": "blob", "url": fmt.Sprintf("%s/blob/home", srv.URL)},
					{"path": "Authentication-Guide.md", "type": "blob", "url": fmt.Sprintf("%s/blob/auth", srv.URL)},
					{"path": "images/logo.png", "type": "blob"}, // should be skipped
				},
			})
		default:
			// Blob content endpoint
			json.NewEncoder(w).Encode(map[string]any{
				"content":  "IyBUZXN0IHBhZ2UgY29udGVudA==", // base64: "# Test page content"
				"encoding": "base64",
			})
		}
	}))
	defer srv.Close()

	a := github.NewAdapter(srv.URL)
	_, ch, err := a.Crawl(context.Background(), config.SourceConfig{
		Type: "github_wiki",
		Repo: "myorg/myrepo",
	}, 42)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	var pages []interface{}
	for p := range ch {
		pages = append(pages, p)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages (Home + Authentication Guide), got %d", len(pages))
	}
}
