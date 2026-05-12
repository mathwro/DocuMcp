package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/mathwro/DocuMcp/internal/crawler"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

// TestTriggerCrawl_Returns409WhenAlreadyCrawling verifies that a second crawl
// request for a source already marked in-flight returns 409 Conflict instead
// of silently spawning a second goroutine.
func TestTriggerCrawl_Returns409WhenAlreadyCrawling(t *testing.T) {
	store := testutil.OpenStoreFile(t)

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

func TestStopCrawl_CancelsInFlightCrawl(t *testing.T) {
	store := testutil.OpenStoreFile(t)

	id, err := store.InsertSource(db.Source{Name: "CrawlMe", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := NewServer(store, nil, nil, make([]byte, 32))
	canceled := false
	srv.crawlingMu.Lock()
	srv.crawlingIDs[id] = true
	srv.crawlCancels[id] = func() { canceled = true }
	srv.crawlingMu.Unlock()

	r := httptest.NewRequest(http.MethodDelete, "/api/sources/"+strconv.FormatInt(id, 10)+"/crawl", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}
	if !canceled {
		t.Fatal("expected crawl cancel function to be called")
	}
	srv.crawlingMu.Lock()
	gotErr := srv.crawlErrors[id]
	srv.crawlingMu.Unlock()
	if gotErr != "crawl stopped by user" {
		t.Fatalf("expected manual stop error, got %q", gotErr)
	}
}

func TestStopCrawl_Returns409WhenNotCrawling(t *testing.T) {
	store := testutil.OpenStoreFile(t)

	id, err := store.InsertSource(db.Source{Name: "Idle", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := NewServer(store, nil, nil, make([]byte, 32))
	r := httptest.NewRequest(http.MethodDelete, "/api/sources/"+strconv.FormatInt(id, 10)+"/crawl", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListSources_IncludesLatestCrawlError(t *testing.T) {
	store := testutil.OpenStoreFile(t)

	id, err := store.InsertSource(db.Source{Name: "Blocked", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := NewServer(store, nil, nil, make([]byte, 32))
	srv.crawlingMu.Lock()
	srv.crawlErrors[id] = "crawl: web crawl blocked by remote site"
	srv.crawlingMu.Unlock()

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var got []sourceResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 source, got %d", len(got))
	}
	if got[0].CrawlError == "" {
		t.Fatal("expected crawl error in source response")
	}
}
