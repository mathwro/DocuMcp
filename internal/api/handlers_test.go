package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/documcp/documcp/internal/api"
	"github.com/documcp/documcp/internal/db"
)

func openTestStore(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestListSources_Empty(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sources []db.Source
	if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sources))
	}
}

func TestListSources(t *testing.T) {
	store := openTestStore(t)

	_, err := store.InsertSource(db.Source{Name: "Docs", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sources []db.Source
	if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "Docs" {
		t.Errorf("expected name 'Docs', got %q", sources[0].Name)
	}
}

func TestCreateSource(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	body, err := json.Marshal(db.Source{Name: "NewDocs", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created db.Source
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "NewDocs" {
		t.Errorf("expected name 'NewDocs', got %q", created.Name)
	}
	if created.ID == 0 {
		t.Errorf("expected non-zero ID after creation")
	}

	// Verify persisted in store.
	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 source in store, got %d", len(sources))
	}
}

func TestCreateSource_BadBody(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDeleteSource(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "ToDelete", Type: "web"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil)
	r := httptest.NewRequest(http.MethodDelete, "/api/sources/"+itoa(id), nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify removed from store.
	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources after delete, got %d", len(sources))
	}
}

func TestDeleteSource_NotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodDelete, "/api/sources/9999", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteSource_BadID(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodDelete, "/api/sources/abc", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTriggerCrawl_SourceNotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodPost, "/api/sources/999/crawl", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTriggerCrawl_NilCrawler(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "CrawlMe", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	// Pass nil crawler — triggerCrawl should still return 202.
	srv := api.NewServer(store, nil, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/sources/"+itoa(id)+"/crawl", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "crawl started" {
		t.Errorf("expected status 'crawl started', got %q", resp["status"])
	}
}

func TestSearchHandler_MissingQuery(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSearchHandler_EmptyResults(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/search?q=golang", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var results []any
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// itoa converts int64 to string for use in URL paths.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
