package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/mathwro/DocuMcp/internal/api"
	"github.com/mathwro/DocuMcp/internal/auth"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/testutil"
)

func openTestStore(t *testing.T) *db.Store {
	t.Helper()
	return testutil.OpenStore(t)
}

func TestListSources_Empty(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

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

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
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
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

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

func TestCreateSource_RejectsDangerousSchemes(t *testing.T) {
	cases := []struct {
		name string
		src  db.Source
	}{
		{"file scheme", db.Source{Name: "f", Type: "web", URL: "file:///etc/passwd"}},
		{"javascript scheme", db.Source{Name: "j", Type: "web", URL: "javascript:alert(1)"}},
		{"gopher scheme", db.Source{Name: "g", Type: "web", URL: "gopher://evil.example.com/"}},
		{"ftp scheme", db.Source{Name: "f", Type: "web", URL: "ftp://files.example.com/"}},
		{"http empty host", db.Source{Name: "e", Type: "web", URL: "http:///path"}},
		{"bare string", db.Source{Name: "b", Type: "web", URL: "not-a-url"}},
		{"azure devops bad base_url", db.Source{Name: "a", Type: "azure_devops", URL: "https://dev.azure.com/org", BaseURL: "javascript:alert(1)"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := openTestStore(t)
			srv := api.NewServer(store, nil, nil, make([]byte, 32))
			body, err := json.Marshal(tc.src)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateSource_AllowsEmptyURLForGitHubTypes(t *testing.T) {
	// github_wiki and github_repo use the `repo` field, not URL.
	for _, srcType := range []string{"github_wiki", "github_repo"} {
		t.Run(srcType, func(t *testing.T) {
			store := openTestStore(t)
			srv := api.NewServer(store, nil, nil, make([]byte, 32))
			body, _ := json.Marshal(db.Source{Name: "g", Type: srcType, Repo: "owner/repo"})
			r := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader(body))
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)
			if w.Code != http.StatusCreated {
				t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateSource_BadBody(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

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

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
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
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodDelete, "/api/sources/9999", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteSource_BadID(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodDelete, "/api/sources/abc", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestUpdateSource(t *testing.T) {
	store := openTestStore(t)
	id, err := store.InsertSource(db.Source{
		Name:          "Old Docs",
		Type:          "web",
		URL:           "https://old.example.com",
		CrawlSchedule: "0 1 * * *",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if err := store.UpdateSourcePageCount(id, 7); err != nil {
		t.Fatalf("UpdateSourcePageCount: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, err := json.Marshal(db.Source{
		Name:          "New Docs",
		Type:          "github_repo",
		URL:           "https://new.example.com",
		IncludePath:   "https://new.example.com/guide/",
		CrawlSchedule: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id), bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated db.Source
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Name != "New Docs" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}
	if updated.Type != "web" {
		t.Errorf("expected type to remain web, got %q", updated.Type)
	}
	if updated.URL != "https://new.example.com" {
		t.Errorf("expected updated URL, got %q", updated.URL)
	}
	if updated.IncludePath != "https://new.example.com/guide/" {
		t.Errorf("expected updated include path, got %q", updated.IncludePath)
	}
	if updated.CrawlSchedule != "0 2 * * *" {
		t.Errorf("expected updated crawl schedule, got %q", updated.CrawlSchedule)
	}
	if updated.PageCount != 7 {
		t.Errorf("expected page count to remain 7, got %d", updated.PageCount)
	}
	if updated.LastCrawled == nil {
		t.Errorf("expected last crawled to remain set")
	}
}

func TestUpdateSource_IncludePaths(t *testing.T) {
	store := openTestStore(t)
	id, err := store.InsertSource(db.Source{
		Name: "Docs",
		Type: "web",
		URL:  "https://docs.example.com",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, err := json.Marshal(db.Source{
		Name:         "Docs",
		URL:          "https://docs.example.com",
		IncludePaths: []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id), bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated db.Source
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"}
	if !reflect.DeepEqual(updated.IncludePaths, want) {
		t.Fatalf("IncludePaths = %#v, want %#v", updated.IncludePaths, want)
	}
	if updated.IncludePath != want[0] {
		t.Fatalf("IncludePath = %q, want first path", updated.IncludePath)
	}
}

func TestCreateSource_IncludePathsRejectsBadURL(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, _ := json.Marshal(db.Source{
		Name:         "Docs",
		Type:         "web",
		URL:          "https://docs.example.com",
		IncludePaths: []string{"https://docs.example.com/guides/", "://bad-url"},
	})

	r := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSource_NotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, _ := json.Marshal(db.Source{Name: "Missing", Type: "web", URL: "https://example.com"})

	r := httptest.NewRequest(http.MethodPut, "/api/sources/9999", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSource_RejectsDangerousSchemes(t *testing.T) {
	store := openTestStore(t)
	id, err := store.InsertSource(db.Source{Name: "Docs", Type: "web", URL: "https://example.com"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, _ := json.Marshal(db.Source{Name: "Docs", Type: "web", URL: "javascript:alert(1)"})

	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id), bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTriggerCrawl_SourceNotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

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
	srv := api.NewServer(store, nil, nil, make([]byte, 32))
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
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSearchHandler_EmptyResults(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

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

// --- Auth endpoint tests ---

func TestAuthStart_NotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodPost, "/api/sources/9999/auth/start", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthStart_BadID(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodPost, "/api/sources/abc/auth/start", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthPoll_NoPendingFlow(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "Wiki", Type: "github_wiki", URL: "https://github.com/org/wiki"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	r := httptest.NewRequest(http.MethodGet, "/api/sources/"+itoa(id)+"/auth/poll", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	// No flow has been started — expect 404.
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthRevoke_NotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodDelete, "/api/sources/9999/auth", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthRevoke_NoToken(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "AzureWiki", Type: "azure_devops", URL: "https://dev.azure.com/org/wiki"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	// Revoking when no token exists should still succeed (idempotent).
	r := httptest.NewRequest(http.MethodDelete, "/api/sources/"+itoa(id)+"/auth", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestIsGitHubFlow verifies the routing predicate used by authStart to decide
// between the GitHub and Microsoft device-code branches.
func TestIsGitHubFlow(t *testing.T) {
	cases := []struct {
		sourceType string
		want       bool
	}{
		{"github_wiki", true},
		{"github_repo", true},
		{"azure_devops", false},
		{"web", false},
		{"", false},
	}
	for _, tc := range cases {
		got := api.IsGitHubFlow(tc.sourceType)
		if got != tc.want {
			t.Errorf("IsGitHubFlow(%q) = %v, want %v", tc.sourceType, got, tc.want)
		}
	}
}

// TestAuthStart_RejectsGitHub verifies that POST /auth/start returns 400 for
// GitHub source types. The device flow has been replaced by a user-supplied
// fine-grained PAT (PUT /auth/token).
func TestAuthStart_RejectsGitHub(t *testing.T) {
	for _, srcType := range []string{"github_wiki", "github_repo"} {
		t.Run(srcType, func(t *testing.T) {
			store := openTestStore(t)
			id, err := store.InsertSource(db.Source{Name: "s", Type: srcType, Repo: "o/r"})
			if err != nil {
				t.Fatalf("InsertSource: %v", err)
			}

			srv := api.NewServer(store, nil, nil, make([]byte, 32))
			r := httptest.NewRequest(http.MethodPost, "/api/sources/"+itoa(id)+"/auth/start", nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// --- AuthSetToken (PUT /api/sources/{id}/auth/token) ---

func TestAuthSetToken_SavesToken(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "Repo", Type: "github_repo", Repo: "octocat/hello-world"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	key := make([]byte, 32)
	srv := api.NewServer(store, nil, nil, key)

	body := bytes.NewReader([]byte(`{"token":"ghp_fine_grained_abc123"}`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id)+"/auth/token", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	ts := auth.NewTokenStore(store, key)
	tok, err := ts.Load(id, "github")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tok.AccessToken != "ghp_fine_grained_abc123" {
		t.Errorf("AccessToken: got %q, want %q", tok.AccessToken, "ghp_fine_grained_abc123")
	}
}

func TestAuthSetToken_NotFound(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	body := bytes.NewReader([]byte(`{"token":"x"}`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/9999/auth/token", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthSetToken_BadID(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	body := bytes.NewReader([]byte(`{"token":"x"}`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/abc/auth/token", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAuthSetToken_EmptyToken(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "Repo", Type: "github_repo", Repo: "o/r"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body := bytes.NewReader([]byte(`{"token":""}`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id)+"/auth/token", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthSetToken_BadBody(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "Repo", Type: "github_repo", Repo: "o/r"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body := bytes.NewReader([]byte(`not-json`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id)+"/auth/token", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthSetToken_ThenRevoke_TokenGone(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "Repo", Type: "github_repo", Repo: "o/r"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	key := make([]byte, 32)
	srv := api.NewServer(store, nil, nil, key)

	// Save a token.
	body := bytes.NewReader([]byte(`{"token":"ghp_abc"}`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id)+"/auth/token", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("set: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	ts := auth.NewTokenStore(store, key)
	if _, err := ts.Load(id, "github"); err != nil {
		t.Fatalf("Load after save: %v", err)
	}

	// Revoke it.
	r = httptest.NewRequest(http.MethodDelete, "/api/sources/"+itoa(id)+"/auth", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Token must be gone.
	if _, err := ts.Load(id, "github"); err == nil {
		t.Fatal("Load after revoke: expected error, got nil")
	}
}

func TestAuthSetToken_RejectsNonGitHub(t *testing.T) {
	store := openTestStore(t)

	id, err := store.InsertSource(db.Source{Name: "Azure", Type: "azure_devops", URL: "https://dev.azure.com/o/p"})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body := bytes.NewReader([]byte(`{"token":"x"}`))
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id)+"/auth/token", body)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
