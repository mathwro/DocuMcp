package search

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// Result represents a single search result with relevance score.
type Result struct {
	URL      string
	Title    string
	Snippet  string
	SourceID int64
	Path     []string
	Score    float64
}

// scanResults scans rows of (url, title, snippet, source_id, path, score) into Result slice.
func scanResults(rows *sql.Rows) ([]Result, error) {
	var results []Result
	for rows.Next() {
		var r Result
		var pathJSON string
		if err := rows.Scan(&r.URL, &r.Title, &r.Snippet, &r.SourceID, &pathJSON, &r.Score); err != nil {
			return nil, err
		}
		r.Snippet = strings.TrimSpace(htmlTagRe.ReplaceAllString(r.Snippet, " "))
		if err := json.Unmarshal([]byte(pathJSON), &r.Path); err != nil {
			slog.Warn("failed to unmarshal page path", "path_json", pathJSON, "err", err)
			r.Path = []string{}
		}
		results = append(results, r)
	}
	if results == nil {
		results = []Result{}
	}
	return results, rows.Err()
}
