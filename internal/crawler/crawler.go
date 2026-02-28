package crawler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/documcp/documcp/internal/adapter"
	_ "github.com/documcp/documcp/internal/adapter/azuredevops"
	_ "github.com/documcp/documcp/internal/adapter/github"
	_ "github.com/documcp/documcp/internal/adapter/web"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
)

// Crawler dispatches crawl jobs to the appropriate adapter, persists pages,
// and optionally computes embeddings for semantic search.
type Crawler struct {
	store    *db.Store
	embedder *embed.Embedder // nil = skip embeddings
}

// New returns a new Crawler. Pass nil for embedder to skip embedding generation.
func New(store *db.Store, embedder *embed.Embedder) *Crawler {
	return &Crawler{store: store, embedder: embedder}
}

// Crawl dispatches to the correct adapter, consumes the pages channel,
// upserts each page (and optionally embeds it), then updates the source page count.
func (c *Crawler) Crawl(ctx context.Context, src db.Source) error {
	a, ok := adapter.Registry[src.Type]
	if !ok {
		return fmt.Errorf("unknown source type: %s", src.Type)
	}

	cfgSrc := sourceToConfig(src)
	pages, err := a.Crawl(ctx, cfgSrc, src.ID)
	if err != nil {
		return fmt.Errorf("crawl: %w", err)
	}

	count := 0
	for page := range pages {
		if err := c.store.UpsertPage(page); err != nil {
			slog.Error("upsert page", "url", page.URL, "err", err)
			continue
		}
		if c.embedder != nil {
			if err := c.indexEmbedding(ctx, page); err != nil {
				slog.Error("embed page", "url", page.URL, "err", err)
			}
		}
		count++
	}
	return c.store.UpdateSourcePageCount(src.ID, count)
}

// indexEmbedding fetches the stored page ID and upserts its embedding.
func (c *Crawler) indexEmbedding(ctx context.Context, page db.Page) error {
	p, err := c.store.GetPageByURL(page.URL)
	if err != nil {
		return err
	}
	vecs, err := c.embedder.Embed([]string{page.Title + " " + page.Content})
	if err != nil {
		return err
	}
	return c.store.UpsertEmbedding(p.ID, vecs[0])
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
