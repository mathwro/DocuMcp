package crawler

import (
	"fmt"

	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
)

// SyncConfigSources persists sources declared in config.yaml so they are
// visible to the Web UI and MCP/API source listings.
func SyncConfigSources(store *db.Store, cfg *config.Config) error {
	for _, src := range cfg.Sources {
		if _, err := store.UpsertSourceConfigByName(configSourceToDB(src)); err != nil {
			return fmt.Errorf("sync config source %q: %w", src.Name, err)
		}
	}
	return nil
}

func configSourceToDB(src config.SourceConfig) db.Source {
	return db.Source{
		Name:          src.Name,
		Type:          src.Type,
		URL:           src.URL,
		Repo:          src.Repo,
		Branch:        src.Branch,
		BaseURL:       src.BaseURL,
		SpaceKey:      src.SpaceKey,
		Auth:          src.Auth,
		CrawlSchedule: src.CrawlSchedule,
		IncludePath:   src.IncludePath,
		IncludePaths:  src.IncludePaths,
		Origin:        db.SourceOriginConfig,
	}
}
