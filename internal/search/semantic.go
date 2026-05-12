package search

import (
	"fmt"
	"reflect"

	"github.com/mathwro/DocuMcp/internal/db"
)

type queryEmbedder interface {
	Embed([]string) ([][]float32, error)
}

// Semantic runs a nearest-neighbour vector search using sqlite-vec.
// Returns nil, nil when embedder is nil — caller skips semantic search gracefully.
func Semantic(store *db.Store, embedder queryEmbedder, query string, limit int) ([]Result, error) {
	if isNilEmbedder(embedder) {
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

func isNilEmbedder(embedder queryEmbedder) bool {
	if embedder == nil {
		return true
	}
	v := reflect.ValueOf(embedder)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
