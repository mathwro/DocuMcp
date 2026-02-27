package db_test

import (
	"testing"

	"github.com/documcp/documcp/internal/db"
)

func TestOpen_CreatesSchema(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
}

func TestInsertAndListSources(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	id, err := store.InsertSource(db.Source{
		Name: "Test Source",
		Type: "web",
		URL:  "https://example.com",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "Test Source" {
		t.Errorf("expected 'Test Source', got %q", sources[0].Name)
	}
	if sources[0].ID != id {
		t.Errorf("expected id %d, got %d", id, sources[0].ID)
	}
}

func TestUpsertAndGetPage(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	sourceID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	err = store.UpsertPage(db.Page{
		SourceID: sourceID,
		URL:      "https://example.com/page",
		Title:    "Test Page",
		Content:  "This is the page content",
		Path:     []string{"Section A", "Test Page"},
	})
	if err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	page, err := store.GetPageByURL("https://example.com/page")
	if err != nil {
		t.Fatalf("GetPageByURL: %v", err)
	}
	if page.Title != "Test Page" {
		t.Errorf("expected 'Test Page', got %q", page.Title)
	}
	if len(page.Path) != 2 || page.Path[0] != "Section A" {
		t.Errorf("unexpected path: %v", page.Path)
	}
}

func TestUpsertPage_UpdatesOnConflict(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	srcID, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "https://example.com/p", Title: "Old Title", Content: "old"})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "https://example.com/p", Title: "New Title", Content: "new"})

	page, err := store.GetPageByURL("https://example.com/p")
	if err != nil {
		t.Fatalf("GetPageByURL: %v", err)
	}
	if page.Title != "New Title" {
		t.Errorf("expected 'New Title' after upsert, got %q", page.Title)
	}
}

func TestDeleteSource(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	id, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err := store.DeleteSource(id); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	sources, _ := store.ListSources()
	if len(sources) != 0 {
		t.Errorf("expected 0 sources after delete, got %d", len(sources))
	}
}

func TestUpdateSourcePageCount(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	id, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err := store.UpdateSourcePageCount(id, 42); err != nil {
		t.Fatalf("UpdateSourcePageCount: %v", err)
	}
	sources, _ := store.ListSources()
	if sources[0].PageCount != 42 {
		t.Errorf("expected page_count 42, got %d", sources[0].PageCount)
	}
}
