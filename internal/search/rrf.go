package search

import "sort"

// MergeRRF combines FTS and semantic results using reciprocal rank fusion.
// k=60 is the standard RRF constant that smooths rank-based scoring.
func MergeRRF(ftsResults, semanticResults []Result, limit int) []Result {
	const k = 60.0
	scores := map[string]float64{}
	byURL := map[string]Result{}

	for i, r := range ftsResults {
		scores[r.URL] += 1.0 / (k + float64(i+1))
		byURL[r.URL] = r
	}
	for i, r := range semanticResults {
		scores[r.URL] += 1.0 / (k + float64(i+1))
		byURL[r.URL] = r
	}

	type scored struct {
		url   string
		score float64
	}
	ranked := make([]scored, 0, len(scores))
	for url, score := range scores {
		ranked = append(ranked, scored{url, score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	results := make([]Result, 0, limit)
	for _, s := range ranked {
		if len(results) >= limit {
			break
		}
		r := byURL[s.url]
		r.Score = s.score
		results = append(results, r)
	}
	return results
}
