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
		WITH knn AS (
		    SELECT page_id, distance
		    FROM page_embeddings
		    WHERE embedding MATCH ?
		      AND k = ?
		)
		SELECT p.url, p.title, p.content, p.source_id, p.path,
		       knn.distance AS score
		FROM knn
		JOIN pages p ON knn.page_id = p.id
		ORDER BY knn.distance
	`, blob, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic query: %w", err)
	}
	defer rows.Close()
	results, err := scanResults(rows)
	if err != nil {
		return nil, fmt.Errorf("scan semantic results: %w", err)
	}
	return results, nil
}
