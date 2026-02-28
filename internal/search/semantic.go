package search

import (
	"fmt"

	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
)

// Semantic runs a nearest-neighbour vector search using sqlite-vec.
// Returns nil, nil when embedder is nil — caller skips semantic search gracefully.
func Semantic(store *db.Store, embedder *embed.Embedder, query string, limit int) ([]Result, error) {
	if embedder == nil {
		return nil, nil
	}
	vecs, err := embedder.Embed([]string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	blob := db.Float32ToBlob(vecs[0])
	rows, err := store.DB().Query(`
		SELECT p.url, p.title, p.content, p.source_id, p.path,
		       pe.distance AS score
		FROM page_embeddings pe
		JOIN pages p ON pe.page_id = p.id
		WHERE pe.embedding MATCH ?
		  AND k = ?
		ORDER BY pe.distance
	`, blob, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic query: %w", err)
	}
	defer rows.Close()
	return scanResults(rows)
}
