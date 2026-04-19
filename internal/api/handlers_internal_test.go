package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/mathwro/DocuMcp/internal/crawler"
	"github.com/mathwro/DocuMcp/internal/db"
)

// TestTriggerCrawl_Returns409WhenAlreadyCrawling verifies that a second crawl
// request for a source already marked in-flight returns 409 Conflict instead
// of silently spawning a second goroutine.
func TestTriggerCrawl_Returns409WhenAlreadyCrawling(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := db.Open(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	id, err := store.InsertSource(db.Source{Name: "CrawlMe", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	// Real crawler instance so the handler takes the "crawler != nil" branch.
	// The handler's conflict check fires before any adapter call runs.
	srv := NewServer(store, crawler.New(store, nil), nil, make([]byte, 32))

	srv.crawlingMu.Lock()
	srv.crawlingIDs[id] = true
	srv.crawlingMu.Unlock()

	r := httptest.NewRequest(http.MethodPost, "/api/sources/"+strconv.FormatInt(id, 10)+"/crawl", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}
