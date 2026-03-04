package search_test

import (
	"testing"

	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

func setupBrowseDB(t *testing.T) (*db.Store, int64) {
	t.Helper()
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	return store, srcID
}

func TestBrowseTopLevel(t *testing.T) {
	store, srcID := setupBrowseDB(t)

	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "Auth Overview", Path: []string{"Authentication", "Auth Overview"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u2", Title: "OAuth Setup", Path: []string{"Authentication", "OAuth Setup"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u3", Title: "Docker Guide", Path: []string{"Deployment", "Docker Guide"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	sections, err := search.BrowseTopLevel(store, srcID)
	if err != nil {
		t.Fatalf("BrowseTopLevel: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d: %+v", len(sections), sections)
	}

	counts := map[string]int{}
	for _, s := range sections {
		counts[s.Name] = s.PageCount
	}
	if counts["Authentication"] != 2 {
		t.Errorf("expected Authentication=2, got %d", counts["Authentication"])
	}
	if counts["Deployment"] != 1 {
		t.Errorf("expected Deployment=1, got %d", counts["Deployment"])
	}
}

func TestBrowseSection(t *testing.T) {
	store, srcID := setupBrowseDB(t)

	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "Auth Overview", Path: []string{"Authentication", "Auth Overview"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u2", Title: "OAuth Setup", Path: []string{"Authentication", "OAuth Setup"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u3", Title: "Docker Guide", Path: []string{"Deployment", "Docker Guide"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	pages, err := search.BrowseSection(store, srcID, "Authentication")
	if err != nil {
		t.Fatalf("BrowseSection: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d: %+v", len(pages), pages)
	}

	// Deployment pages should NOT appear
	for _, p := range pages {
		if p.Path[0] != "Authentication" {
			t.Errorf("unexpected page from wrong section: %v", p)
		}
	}
}

func TestBrowseTopLevel_EmptySource(t *testing.T) {
	store, srcID := setupBrowseDB(t)

	sections, err := search.BrowseTopLevel(store, srcID)
	if err != nil {
		t.Fatalf("BrowseTopLevel: %v", err)
	}
	if len(sections) != 0 {
		t.Errorf("expected 0 sections for empty source, got %d", len(sections))
	}
}

func TestBrowseSection_NoMatch(t *testing.T) {
	store, srcID := setupBrowseDB(t)

	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "Auth Overview", Path: []string{"Authentication", "Auth Overview"}}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	pages, err := search.BrowseSection(store, srcID, "NonExistent")
	if err != nil {
		t.Fatalf("BrowseSection: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected 0 pages, got %d", len(pages))
	}
}
