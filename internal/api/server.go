package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/documcp/documcp/internal/auth"
	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/web"
)

// pendingFlow holds an in-progress device code flow and the metadata needed to
// complete it.  Only one of msFlow / ghFlow will be non-nil.
type pendingFlow struct {
	msFlow   *auth.MicrosoftDeviceFlow
	ghFlow   *auth.GitHubDeviceFlow
	provider string
	clientID string // only used by GitHub flows
}

// Server is the REST API server for the Web UI.
type Server struct {
	store        *db.Store
	crawler      *crawler.Crawler
	mcpHandler   http.Handler
	mux          *http.ServeMux
	handler      http.Handler // full middleware chain, built once in NewServer
	tokenStore   *auth.TokenStore
	pendingFlows map[int64]*pendingFlow
	flowMu       sync.Mutex
	crawlingIDs  map[int64]bool // source IDs with an active crawl goroutine
	crawlingMu   sync.Mutex
	cancel       context.CancelFunc // cancels background work on Shutdown
	ctx          context.Context    // cancelled when Shutdown is called
}

// NewServer creates a new API server wired to the given store, crawler, and MCP handler.
// Pass nil for mcpHandler and/or crawler when not needed (e.g. in tests).
// key must be exactly 32 bytes and is used for AES-256-GCM token encryption.
func NewServer(store *db.Store, c *crawler.Crawler, mcpHandler http.Handler, key []byte) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:        store,
		crawler:      c,
		mcpHandler:   mcpHandler,
		mux:          http.NewServeMux(),
		tokenStore:   auth.NewTokenStore(store, key),
		pendingFlows: make(map[int64]*pendingFlow),
		crawlingIDs:  make(map[int64]bool),
		ctx:          ctx,
		cancel:       cancel,
	}
	s.routes()
	// Build the middleware chain once at construction time.
	apiKey := os.Getenv("DOCUMCP_API_KEY")
	s.handler = securityMiddleware(apiKeyMiddleware(apiKey, s.mux))
	return s
}

// Shutdown cancels background operations (e.g. in-progress crawls) started by
// this server. It should be called during process shutdown after the HTTP
// server has stopped accepting new requests.
func (s *Server) Shutdown() {
	s.cancel()
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/sources", s.listSources)
	s.mux.HandleFunc("POST /api/sources", s.createSource)
	s.mux.HandleFunc("DELETE /api/sources/{id}", s.deleteSource)
	s.mux.HandleFunc("POST /api/sources/{id}/crawl", s.triggerCrawl)
	s.mux.HandleFunc("GET /api/search", s.searchHandler)
	s.mux.HandleFunc("POST /api/sources/{id}/auth/start", s.authStart)
	s.mux.HandleFunc("GET /api/sources/{id}/auth/poll", s.authPoll)
	s.mux.HandleFunc("DELETE /api/sources/{id}/auth", s.authRevoke)
	if s.mcpHandler != nil {
		s.mux.Handle("/mcp/", s.mcpHandler)
	}
	s.mux.Handle("/", http.FileServer(web.FileSystem()))
}

// apiKeyMiddleware enforces Bearer token auth on /api/* and /mcp/* paths.
// If DOCUMCP_API_KEY is empty the middleware is a no-op (open access, with a
// warning logged at startup from main.go).
func apiKeyMiddleware(apiKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey != "" {
			path := r.URL.Path
			if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/mcp/") {
				got := r.Header.Get("Authorization")
				if got != "Bearer "+apiKey {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"}) //nolint:errcheck
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// securityMiddleware adds HTTP security headers to all responses and enforces
// a restrictive CORS policy (only loopback origins are permitted).
func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'")

		// CORS: only allow cross-origin requests from loopback addresses.
		// Requests from external sites are denied by omitting the header.
		if origin := r.Header.Get("Origin"); origin != "" {
			if isLoopbackOrigin(origin) {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isLoopbackOrigin returns true when the origin URL's host is a loopback address.
func isLoopbackOrigin(origin string) bool {
	// Strip scheme to get host (origin is scheme://host[:port])
	after, ok := strings.CutPrefix(origin, "http://")
	if !ok {
		after, _ = strings.CutPrefix(origin, "https://")
	}
	// Remove port if present
	host, _, err := net.SplitHostPort(after)
	if err != nil {
		host = after // no port
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// logAPIKeyWarning logs a startup warning when no API key is configured.
// Call once from main after creating the server.
func LogAPIKeyWarning() {
	if os.Getenv("DOCUMCP_API_KEY") == "" {
		slog.Warn("DOCUMCP_API_KEY not set — API and MCP endpoints are unauthenticated")
	}
}
