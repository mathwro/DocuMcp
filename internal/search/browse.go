package search

import (
	"encoding/json"
	"fmt"

	"github.com/documcp/documcp/internal/db"
)

// browseSectionLimit is the maximum number of pages returned by BrowseSection.
// Callers needing more pages should narrow the section or use search_docs.
const browseSectionLimit = 50

// Section is a top-level group of pages within a source.
type Section struct {
	Name      string
	PageCount int
}

// PageRef is a lightweight page reference returned by BrowseSection.
type PageRef struct {
	URL   string
	Title string
	Path  []string
}

// BrowseTopLevel returns the distinct top-level sections for a source,
// with the page count per section. Returns make([]Section, 0) for an empty source.
func BrowseTopLevel(store *db.Store, sourceID int64) ([]Section, error) {
	rows, err := store.DB().Query(
		`SELECT json_extract(path, '$[0]') AS section, COUNT(*) AS cnt
		 FROM pages
		 WHERE source_id = ? AND json_extract(path, '$[0]') IS NOT NULL
		 GROUP BY section`,
		sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("browse top level: %w", err)
	}
	defer rows.Close()

	sections := make([]Section, 0)
	for rows.Next() {
		var s Section
		if err := rows.Scan(&s.Name, &s.PageCount); err != nil {
			return nil, fmt.Errorf("browse top level scan: %w", err)
		}
		sections = append(sections, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse top level rows: %w", err)
	}
	return sections, nil
}

// BrowseSection returns up to 50 pages whose first path element matches section.
// Returns make([]PageRef, 0) when there are no matches.
func BrowseSection(store *db.Store, sourceID int64, section string) ([]PageRef, error) {
	rows, err := store.DB().Query(
		`SELECT url, title, path FROM pages
		 WHERE source_id = ? AND json_extract(path, '$[0]') = ?
		 LIMIT ?`,
		sourceID, section, browseSectionLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("browse section: %w", err)
	}
	defer rows.Close()

	pages := make([]PageRef, 0)
	for rows.Next() {
		var ref PageRef
		var pathJSON string
		if err := rows.Scan(&ref.URL, &ref.Title, &pathJSON); err != nil {
			return nil, fmt.Errorf("browse section scan: %w", err)
		}
		if err := json.Unmarshal([]byte(pathJSON), &ref.Path); err != nil {
			return nil, fmt.Errorf("browse section unmarshal path %q: %w", ref.URL, err)
		}
		pages = append(pages, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("browse section rows: %w", err)
	}
	return pages, nil
}
