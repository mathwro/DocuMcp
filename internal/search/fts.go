package search

import (
	"fmt"

	"github.com/documcp/documcp/internal/db"
)

// FTS performs a full-text search using SQLite FTS5 BM25 ranking.
// Returns up to limit results ordered by relevance (best first).
//
// query is passed directly to the FTS5 MATCH operator and may contain
// FTS5 syntax (AND, OR, NOT, prefix wildcards, column filters). Callers
// are responsible for validating or sanitizing user-supplied input.
func FTS(store *db.Store, query string, limit int) ([]Result, error) {
	// bm25() returns negative values — lower is more relevant, so ORDER BY score ASC
	rows, err := store.DB().Query(`
		SELECT p.url, p.title, p.content, p.source_id, p.path,
		       bm25(pages_fts) AS score
		FROM pages_fts
		JOIN pages p ON pages_fts.rowid = p.id
		WHERE pages_fts MATCH ?
		ORDER BY score
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()
	return scanResults(rows)
}
