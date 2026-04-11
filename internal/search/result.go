package search

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// snippetMaxChars is the maximum character length for a search result snippet.
// Semantic search results are truncated to this length; FTS results use the
// SQL snippet() function which produces a similar-sized excerpt.
const snippetMaxChars = 500

// TruncateSnippet caps s at snippetMaxChars characters, appending "..." if truncated.
// Exported so it can be tested directly.
func TruncateSnippet(s string) string {
	runes := []rune(s)
	if len(runes) <= snippetMaxChars {
		return s
	}
	return string(runes[:snippetMaxChars]) + "..."
}

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
		r.Snippet = TruncateSnippet(strings.TrimSpace(htmlTagRe.ReplaceAllString(r.Snippet, " ")))
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
