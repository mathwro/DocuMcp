package azuredevops_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathwro/DocuMcp/internal/adapter/azuredevops"
	"github.com/mathwro/DocuMcp/internal/config"
)

func TestAzureDevOpsAdapter_CrawlsWikiPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("path") == "/" || r.URL.Query().Get("path") == "":
			// Root pages list
			json.NewEncoder(w).Encode(map[string]any{
				"value": []map[string]any{
					{"path": "/Home", "id": "page1"},
					{"path": "/Architecture/Overview", "id": "page2"},
				},
			})
		default:
			// Individual page content
			json.NewEncoder(w).Encode(map[string]any{
				"path":    r.URL.Query().Get("path"),
				"content": "# Test page\n\nSome content here.",
			})
		}
	}))
	defer srv.Close()

	a := azuredevops.NewAdapter(srv.URL)
	_, ch, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:     "azure_devops",
		BaseURL:  srv.URL,
		SpaceKey: "myorg/myproject/_apis/wiki/wikis/mywiki",
	}, 42)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	var pages []interface{}
	for p := range ch {
		pages = append(pages, p)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
}
