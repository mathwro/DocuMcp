package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathwro/DocuMcp/internal/api"
)

func TestAPIKeyMiddleware_ValidKey(t *testing.T) {
	t.Setenv("DOCUMCP_API_KEY", "correct-horse-battery-staple")
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	r.Header.Set("Authorization", "Bearer correct-horse-battery-staple")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKeyMiddleware_RejectsWrongKey_SameLength(t *testing.T) {
	// Same length as the real key — this is the case a timing-unsafe compare
	// would leak. The regression guard is that we still reject, with no
	// observable difference from a length-mismatched reject.
	t.Setenv("DOCUMCP_API_KEY", "correct-horse-battery-staple")
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	r.Header.Set("Authorization", "Bearer incorrect-horse-battery-stap")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKeyMiddleware_RejectsWrongKey_DifferentLength(t *testing.T) {
	t.Setenv("DOCUMCP_API_KEY", "correct-horse-battery-staple")
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	r.Header.Set("Authorization", "Bearer short")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKeyMiddleware_MissingHeader(t *testing.T) {
	t.Setenv("DOCUMCP_API_KEY", "correct-horse-battery-staple")
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKeyMiddleware_OpenAccessWhenUnset(t *testing.T) {
	t.Setenv("DOCUMCP_API_KEY", "")
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKeyMiddleware_StaticFilesOpen(t *testing.T) {
	// Web UI assets must be reachable without Authorization even when an API
	// key is configured. (They cannot send a Bearer header and must still load.)
	t.Setenv("DOCUMCP_API_KEY", "correct-horse-battery-staple")
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code == http.StatusUnauthorized {
		t.Errorf("static file handler should not require auth; got 401")
	}
}
