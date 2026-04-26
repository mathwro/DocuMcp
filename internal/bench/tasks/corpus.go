// internal/bench/tasks/corpus.go
package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// LoadCorpus parses questions.json and validates every entry against knownSources
// (the set of source names returned by the running DocuMcp's GET /api/sources).
// Returns Questions with urlRegex populated.
func LoadCorpus(path string, knownSources map[string]bool) ([]Question, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var qs []Question
	if err := json.Unmarshal(raw, &qs); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	seen := make(map[string]bool, len(qs))
	for i := range qs {
		q := &qs[i]
		if q.ID == "" {
			return nil, fmt.Errorf("entry %d: id is required", i)
		}
		if seen[q.ID] {
			return nil, fmt.Errorf("duplicate id: %s", q.ID)
		}
		seen[q.ID] = true
		if q.Tier < 1 || q.Tier > 3 {
			return nil, fmt.Errorf("%s: tier must be 1, 2, or 3 (got %d)", q.ID, q.Tier)
		}
		if q.Question == "" {
			return nil, fmt.Errorf("%s: question is required", q.ID)
		}
		if !knownSources[q.ExpectedSource] {
			return nil, fmt.Errorf("%s: expected_source %q not found in DocuMcp instance", q.ID, q.ExpectedSource)
		}
		re, err := regexp.Compile(q.ExpectedURLPattern)
		if err != nil {
			return nil, fmt.Errorf("%s: expected_url_pattern: %w", q.ID, err)
		}
		q.urlRegex = re
		if q.ReferenceExcerpt == "" {
			return nil, fmt.Errorf("%s: reference_excerpt is required", q.ID)
		}
	}
	return qs, nil
}
