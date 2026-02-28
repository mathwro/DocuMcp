package adapter

import (
	"context"

	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

// Adapter is implemented by each documentation source type.
type Adapter interface {
	// Crawl fetches all pages from the source and sends them to the returned channel.
	// The channel is closed when crawling is complete or ctx is cancelled.
	Crawl(ctx context.Context, source config.SourceConfig, sourceID int64) (<-chan db.Page, error)
	// NeedsAuth returns true if the source requires authentication before crawling.
	NeedsAuth(source config.SourceConfig) bool
}

// Registry maps source type strings to their adapter implementations.
var Registry = map[string]Adapter{}

// Register adds an adapter to the registry. Called from adapter package init() functions.
func Register(sourceType string, a Adapter) {
	Registry[sourceType] = a
}
