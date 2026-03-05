package web

import (
	"context"
	"testing"

	"github.com/documcp/documcp/internal/config"
)

func TestCrawl_IncludePath_InvalidURL(t *testing.T) {
	a := &WebAdapter{}
	_, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:        "web",
		URL:         "https://example.com/docs/",
		IncludePath: "://bad-url",
	}, 1)
	if err == nil {
		t.Fatal("expected error for invalid include_path, got nil")
	}
}

func TestCrawl_IncludePath_CrossOrigin(t *testing.T) {
	a := &WebAdapter{}
	_, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:        "web",
		URL:         "https://example.com/docs/",
		IncludePath: "https://evil.com/attack/",
	}, 1)
	if err == nil {
		t.Fatal("expected error for cross-origin include_path, got nil")
	}
}
