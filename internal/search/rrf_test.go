package search_test

import (
	"testing"

	"github.com/mathwro/DocuMcp/internal/search"
)

func makeResult(url string, score float64) search.Result {
	return search.Result{URL: url, Title: url, Score: score}
}

// TestMergeRRF_SharedURLScoresHigher verifies that a URL appearing in both
// FTS and semantic results receives a higher RRF score than one in only one list.
func TestMergeRRF_SharedURLScoresHigher(t *testing.T) {
	fts := []search.Result{
		makeResult("shared", 0.9),
		makeResult("fts-only", 0.5),
	}
	sem := []search.Result{
		makeResult("shared", 0.8),
		makeResult("sem-only", 0.4),
	}

	results := search.MergeRRF(fts, sem, 10)

	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].URL != "shared" {
		t.Errorf("expected shared URL first (highest RRF score), got %q", results[0].URL)
	}

	// Find scores for shared vs single-list URLs
	var sharedScore, ftsOnlyScore, semOnlyScore float64
	for _, r := range results {
		switch r.URL {
		case "shared":
			sharedScore = r.Score
		case "fts-only":
			ftsOnlyScore = r.Score
		case "sem-only":
			semOnlyScore = r.Score
		}
	}
	if sharedScore <= ftsOnlyScore {
		t.Errorf("shared score %.6f should be > fts-only score %.6f", sharedScore, ftsOnlyScore)
	}
	if sharedScore <= semOnlyScore {
		t.Errorf("shared score %.6f should be > sem-only score %.6f", sharedScore, semOnlyScore)
	}
}

// TestMergeRRF_LimitRespected verifies that MergeRRF returns at most limit results.
func TestMergeRRF_LimitRespected(t *testing.T) {
	fts := []search.Result{
		makeResult("a", 0.9),
		makeResult("b", 0.8),
		makeResult("c", 0.7),
	}
	sem := []search.Result{
		makeResult("d", 0.9),
		makeResult("e", 0.8),
		makeResult("f", 0.7),
	}

	results := search.MergeRRF(fts, sem, 2)
	if len(results) != 2 {
		t.Errorf("expected exactly 2 results (limit enforced), got %d", len(results))
	}
}

// TestMergeRRF_Ordering verifies that results are returned highest-score first.
func TestMergeRRF_Ordering(t *testing.T) {
	// URL "top" ranks #1 in both lists — should have the highest RRF score.
	// URL "mid" ranks #2 in both lists.
	// URL "low" ranks #3 in both lists.
	fts := []search.Result{
		makeResult("top", 0.9),
		makeResult("mid", 0.6),
		makeResult("low", 0.3),
	}
	sem := []search.Result{
		makeResult("top", 0.9),
		makeResult("mid", 0.6),
		makeResult("low", 0.3),
	}

	results := search.MergeRRF(fts, sem, 10)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i := 0; i < len(results)-1; i++ {
		if results[i].Score < results[i+1].Score {
			t.Errorf("results not ordered: results[%d].Score=%.6f < results[%d].Score=%.6f",
				i, results[i].Score, i+1, results[i+1].Score)
		}
	}
	if results[0].URL != "top" {
		t.Errorf("expected 'top' first, got %q", results[0].URL)
	}
}

// TestMergeRRF_EmptyInputs verifies graceful handling of empty slices.
func TestMergeRRF_EmptyInputs(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		results := search.MergeRRF(nil, nil, 10)
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("only fts", func(t *testing.T) {
		fts := []search.Result{makeResult("a", 0.9)}
		results := search.MergeRRF(fts, nil, 10)
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("only semantic", func(t *testing.T) {
		sem := []search.Result{makeResult("b", 0.8)}
		results := search.MergeRRF(nil, sem, 10)
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})
}
