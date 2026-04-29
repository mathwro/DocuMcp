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

func TestMCPTransportRoutes(t *testing.T) {
	store := openTestStore(t)
	sseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("sse")) //nolint:errcheck
	})
	streamableHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("streamable")) //nolint:errcheck
	})
	srv := api.NewServerWithMCPHandlers(store, nil, sseHandler, streamableHandler, make([]byte, 32))

	tests := []struct {
		path string
		want string
	}{
		{path: "/mcp/sse", want: "sse"},
		{path: "/mcp/http", want: "streamable"},
		{path: "/mcp/", want: "sse"},
		{path: "/mcp", want: "streamable"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.path, nil)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if got := w.Body.String(); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}
