package crawler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/documcp/documcp/internal/adapter"
	_ "github.com/documcp/documcp/internal/adapter/azuredevops"
	_ "github.com/documcp/documcp/internal/adapter/github"
	_ "github.com/documcp/documcp/internal/adapter/web"
	"github.com/documcp/documcp/internal/auth"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
)

// Crawler dispatches crawl jobs to the appropriate adapter, persists pages,
// and optionally computes embeddings for semantic search.
type Crawler struct {
	store      *db.Store
	embedder   *embed.Embedder  // nil = skip embeddings
	tokenStore *auth.TokenStore // nil = no token loading
}

// New returns a new Crawler. Pass nil for embedder to skip embedding generation.
func New(store *db.Store, embedder *embed.Embedder) *Crawler {
	return &Crawler{store: store, embedder: embedder}
}

// WithTokenStore sets the token store used to load OAuth tokens for adapters
// that require authentication. It returns the Crawler for chaining.
func (c *Crawler) WithTokenStore(ts *auth.TokenStore) *Crawler {
	c.tokenStore = ts
	return c
}

// providerForType returns the OAuth provider name for a given source type,
// or empty string if the type does not require OAuth.
func providerForType(sourceType string) string {
	switch sourceType {
	case "github_wiki":
		return "github"
	case "azure_devops":
		return "microsoft"
	default:
		return ""
	}
}

// Crawl dispatches to the correct adapter, consumes the pages channel,
// upserts each page (and optionally embeds it), then updates the source page count.
func (c *Crawler) Crawl(ctx context.Context, src db.Source) error {
	a, ok := adapter.Registry[src.Type]
	if !ok {
		return fmt.Errorf("unknown source type: %s", src.Type)
	}

	cfgSrc := sourceToConfig(src)

	// Load OAuth token from the store and inject it into the source config so
	// adapters can use it without calling stub functions.
	if c.tokenStore != nil {
		if provider := providerForType(src.Type); provider != "" {
			token, err := c.tokenStore.Load(src.ID, provider)
			if err != nil {
				if errors.Is(err, db.ErrNotFound) {
					slog.Warn("crawler: no token stored for source, proceeding unauthenticated",
						"source_id", src.ID, "provider", provider)
				} else {
					slog.Error("crawler: load token failed, proceeding unauthenticated",
						"source_id", src.ID, "provider", provider, "err", err)
				}
			} else {
				cfgSrc.Token = token.AccessToken
			}
		}
	}

	total, pages, err := a.Crawl(ctx, cfgSrc, src.ID)
	if err != nil {
		return fmt.Errorf("crawl: %w", err)
	}

	// Reset progress counters so the UI shows 0/total at crawl start.
	if err := c.store.UpdateSourceCrawlTotal(src.ID, total); err != nil {
		slog.Warn("crawler: set crawl total failed", "err", err)
	}
	if err := c.store.UpdateSourcePageCount(src.ID, 0); err != nil {
		slog.Warn("crawler: reset page count failed", "err", err)
	}

	count := 0
	for page := range pages {
		pageID, err := c.store.UpsertPage(page)
		if err != nil {
			slog.Error("upsert page", "url", page.URL, "err", err)
			continue
		}
		if c.embedder != nil {
			if err := c.indexEmbedding(ctx, pageID, page); err != nil {
				slog.Error("embed page", "url", page.URL, "err", err)
			}
		}
		count++
		// Flush progress to DB every 10 pages so the UI can poll it in real-time.
		if count%10 == 0 {
			if err := c.store.UpdateSourcePageCount(src.ID, count); err != nil {
				slog.Warn("crawler: incremental page count update failed", "err", err)
			}
		}
	}
	return c.store.UpdateSourcePageCount(src.ID, count)
}

// indexEmbedding generates and stores the embedding vector for a page.
func (c *Crawler) indexEmbedding(ctx context.Context, pageID int64, page db.Page) error {
	vecs, err := c.embedder.Embed([]string{page.Title + " " + page.Content})
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	return c.store.UpsertEmbedding(pageID, vecs[0])
}

// sourceToConfig maps a db.Source to a config.SourceConfig for adapter dispatch.
func sourceToConfig(src db.Source) config.SourceConfig {
	return config.SourceConfig{
		Name:          src.Name,
		Type:          src.Type,
		Repo:          src.Repo,
		URL:           src.URL,
		Auth:          src.Auth,
		BaseURL:       src.BaseURL,
		SpaceKey:      src.SpaceKey,
		CrawlSchedule: src.CrawlSchedule,
	}
}
