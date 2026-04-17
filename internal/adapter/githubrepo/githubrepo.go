// Package githubrepo indexes .md, .mdx, and .txt files from a GitHub
// repository (optionally scoped to a subfolder) by streaming a single
// tarball download through archive/tar.
package githubrepo

import (
	"context"

	"github.com/documcp/documcp/internal/adapter"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

func init() {
	adapter.Register("github_repo", NewAdapter("https://api.github.com"))
}

// Adapter streams a GitHub repo tarball and emits db.Page entries for
// matching documentation files.
type Adapter struct{ baseURL string }

// NewAdapter constructs an Adapter with the given GitHub API base URL.
// The baseURL parameter enables test injection of a local httptest server.
func NewAdapter(baseURL string) *Adapter {
	return &Adapter{baseURL: baseURL}
}

// NeedsAuth always returns true: private repos require a token, and
// returning true surfaces the auth-setup UI for all github_repo sources.
func (a *Adapter) NeedsAuth(src config.SourceConfig) bool { return true }

// Crawl will stream the tarball and emit pages. Not implemented yet.
func (a *Adapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (int, <-chan db.Page, error) {
	ch := make(chan db.Page)
	close(ch)
	return 0, ch, nil
}
