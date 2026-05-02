package db_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

func TestSource_MarshalJSON_OmitsAuthToken(t *testing.T) {
	src := db.Source{ID: 1, Name: "s", Type: "github_repo", Auth: "ghp_supersecrettoken"}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(b), "ghp_supersecrettoken") {
		t.Errorf("Auth token leaked into JSON: %s", b)
	}
	if strings.Contains(string(b), `"Auth"`) {
		t.Errorf("Auth field key present in JSON: %s", b)
	}
}

func TestOpen_CreatesSchema(t *testing.T) {
	_ = testutil.OpenStore(t)
}

func TestInsertAndListSources(t *testing.T) {
	store := testutil.OpenStore(t)

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
	store := testutil.OpenStore(t)

	sourceID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	_, err = store.UpsertPage(db.Page{
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
	store := testutil.OpenStore(t)

	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "https://example.com/p", Title: "Old Title", Content: "old"}); err != nil {
		t.Fatalf("UpsertPage (first): %v", err)
	}
	if _, err := store.UpsertPage(db.Page{SourceID: srcID, URL: "https://example.com/p", Title: "New Title", Content: "new"}); err != nil {
		t.Fatalf("UpsertPage (second): %v", err)
	}

	page, err := store.GetPageByURL("https://example.com/p")
	if err != nil {
		t.Fatalf("GetPageByURL: %v", err)
	}
	if page.Title != "New Title" {
		t.Errorf("expected 'New Title' after upsert, got %q", page.Title)
	}
}

func TestDeleteSource(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if err := store.DeleteSource(id); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources after delete, got %d", len(sources))
	}
}

func TestUpdateSourceConfig(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{
		Name:          "Original",
		Type:          "web",
		URL:           "https://old.example.com",
		CrawlSchedule: "0 1 * * *",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if err := store.UpdateSourcePageCount(id, 12); err != nil {
		t.Fatalf("UpdateSourcePageCount: %v", err)
	}

	err = store.UpdateSourceConfig(id, db.Source{
		Name:          "Updated",
		Type:          "github_repo",
		URL:           "https://new.example.com",
		Repo:          "owner/repo",
		Branch:        "develop",
		IncludePath:   "docs/",
		CrawlSchedule: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("UpdateSourceConfig: %v", err)
	}

	src, err := store.GetSource(id)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if src.Name != "Updated" {
		t.Errorf("expected name to update, got %q", src.Name)
	}
	if src.Type != "web" {
		t.Errorf("expected type to remain web, got %q", src.Type)
	}
	if src.URL != "https://new.example.com" {
		t.Errorf("expected URL to update, got %q", src.URL)
	}
	if src.Repo != "owner/repo" {
		t.Errorf("expected repo to update, got %q", src.Repo)
	}
	if src.Branch != "develop" {
		t.Errorf("expected branch to update, got %q", src.Branch)
	}
	if src.IncludePath != "docs/" {
		t.Errorf("expected include path to update, got %q", src.IncludePath)
	}
	if src.CrawlSchedule != "0 2 * * *" {
		t.Errorf("expected crawl schedule to update, got %q", src.CrawlSchedule)
	}
	if src.PageCount != 12 {
		t.Errorf("expected page count to remain 12, got %d", src.PageCount)
	}
	if src.LastCrawled == nil {
		t.Errorf("expected last crawled to remain set")
	}
}

func TestUpdateSourcePageCount(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if err := store.UpdateSourcePageCount(id, 42); err != nil {
		t.Fatalf("UpdateSourcePageCount: %v", err)
	}
	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if sources[0].PageCount != 42 {
		t.Errorf("expected page_count 42, got %d", sources[0].PageCount)
	}
}

func TestGetSource(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{Name: "Test", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	src, err := store.GetSource(id)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if src.Name != "Test" {
		t.Errorf("expected 'Test', got %q", src.Name)
	}
}

func TestGetSourceIDByName(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{Name: "Named Source", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	gotID, err := store.GetSourceIDByName("Named Source")
	if err != nil {
		t.Fatalf("GetSourceIDByName: %v", err)
	}
	if gotID != id {
		t.Errorf("expected id %d, got %d", id, gotID)
	}
}

func TestUpsertAndGetToken(t *testing.T) {
	store := testutil.OpenStore(t)

	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	tokenData := []byte("encrypted-token-data")
	expiresAt := time.Now().Add(time.Hour)
	if err := store.UpsertToken(srcID, "microsoft", tokenData, expiresAt); err != nil {
		t.Fatalf("UpsertToken: %v", err)
	}

	data, err := store.GetToken(srcID, "microsoft")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if string(data) != string(tokenData) {
		t.Errorf("expected %q, got %q", tokenData, data)
	}
}

func TestUpsertEmbedding(t *testing.T) {
	store := testutil.OpenStore(t)

	srcID, err := store.InsertSource(db.Source{Name: "S", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	if _, err := store.UpsertPage(db.Page{
		SourceID: srcID,
		URL:      "https://example.com/embed-page",
		Title:    "Embed Page",
		Content:  "content for embedding",
	}); err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	page, err := store.GetPageByURL("https://example.com/embed-page")
	if err != nil {
		t.Fatalf("GetPageByURL: %v", err)
	}

	vec := make([]float32, 384)
	for i := range vec {
		vec[i] = float32(i) / 384.0
	}

	if err := store.UpsertEmbedding(page.ID, vec); err != nil {
		t.Fatalf("UpsertEmbedding (first): %v", err)
	}

	// Second call should be idempotent (INSERT OR REPLACE)
	if err := store.UpsertEmbedding(page.ID, vec); err != nil {
		t.Fatalf("UpsertEmbedding (second, idempotent): %v", err)
	}
}

func TestGetPageByURL_NotFound(t *testing.T) {
	store := testutil.OpenStore(t)

	_, err := store.GetPageByURL("https://nonexistent.example.com")
	if err == nil {
		t.Fatal("expected error for nonexistent URL, got nil")
	}
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestInsertSource_github_repo_persists_branch(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{
		Name:        "example",
		Type:        "github_repo",
		Repo:        "owner/example",
		Branch:      "develop",
		IncludePath: "docs/",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	got, err := store.GetSource(id)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.Branch != "develop" {
		t.Errorf("Branch: got %q, want %q", got.Branch, "develop")
	}
	if got.IncludePath != "docs/" {
		t.Errorf("IncludePath: got %q, want %q", got.IncludePath, "docs/")
	}
}
