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

func TestIsLoopbackOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{origin: "http://localhost:8080", want: true},
		{origin: "https://127.0.0.1:3000", want: true},
		{origin: "http://[::1]:8080", want: true},
		{origin: "https://192.168.1.10:3000", want: false},
		{origin: "https://example.com", want: false},
		{origin: "not a url", want: false},
	}
	for _, tc := range cases {
		if got := isLoopbackOrigin(tc.origin); got != tc.want {
			t.Fatalf("isLoopbackOrigin(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

func TestSecurityMiddlewareAddsHeadersAndAllowsLoopbackCORS(t *testing.T) {
	nextCalled := false
	handler := securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusTeapot)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if !nextCalled {
		t.Fatal("expected wrapped handler to be called")
	}
	if w.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", w.Code)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %#v", w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want loopback origin", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q, want Origin", w.Header().Get("Vary"))
	}
}

func TestSecurityMiddlewareOmitsCORSForExternalOriginAndHandlesPreflight(t *testing.T) {
	nextCalled := false
	handler := securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	r := httptest.NewRequest(http.MethodOptions, "/api/sources", nil)
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if nextCalled {
		t.Fatal("OPTIONS preflight should not call wrapped handler")
	}
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for external origin", got)
	}
}

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
