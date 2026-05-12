package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/mathwro/DocuMcp/internal/auth"
	"github.com/mathwro/DocuMcp/internal/crawler"
	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/web"
)

// pendingFlow holds an in-progress Microsoft device-code flow.
type pendingFlow struct {
	msFlow   *auth.MicrosoftDeviceFlow
	provider string
}

// Server is the REST API server for the Web UI.
type Server struct {
	store                *db.Store
	crawler              *crawler.Crawler
	mcpHandler           http.Handler
	mcpStreamableHandler http.Handler
	mux                  *http.ServeMux
	handler              http.Handler // full middleware chain, built once in NewServer
	tokenStore           *auth.TokenStore
	pendingFlows         map[int64]*pendingFlow
	flowMu               sync.Mutex
	crawlingIDs          map[int64]bool // source IDs with an active crawl goroutine
	crawlCancels         map[int64]context.CancelFunc
	crawlErrors          map[int64]string
	crawlingMu           sync.Mutex
	cancel               context.CancelFunc // cancels background work on Shutdown
	ctx                  context.Context    // cancelled when Shutdown is called
}

// NewServer creates a new API server wired to the given store, crawler, and MCP handler.
// Pass nil for mcpHandler and/or crawler when not needed (e.g. in tests).
// key must be exactly 32 bytes and is used for AES-256-GCM token encryption.
func NewServer(store *db.Store, c *crawler.Crawler, mcpHandler http.Handler, key []byte) *Server {
	return NewServerWithMCPHandlers(store, c, mcpHandler, nil, key)
}

// NewServerWithMCPHandlers creates a new API server with separate MCP handlers
// for SSE (/mcp/sse) and streamable HTTP (/mcp/http) clients.
func NewServerWithMCPHandlers(store *db.Store, c *crawler.Crawler, mcpHandler, mcpStreamableHandler http.Handler, key []byte) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		store:                store,
		crawler:              c,
		mcpHandler:           mcpHandler,
		mcpStreamableHandler: mcpStreamableHandler,
		mux:                  http.NewServeMux(),
		tokenStore:           auth.NewTokenStore(store, key),
		pendingFlows:         make(map[int64]*pendingFlow),
		crawlingIDs:          make(map[int64]bool),
		crawlCancels:         make(map[int64]context.CancelFunc),
		crawlErrors:          make(map[int64]string),
		ctx:                  ctx,
		cancel:               cancel,
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
	s.mux.HandleFunc("GET /api/sources/export", s.exportSources)
	s.mux.HandleFunc("POST /api/sources", s.createSource)
	s.mux.HandleFunc("PUT /api/sources/{id}", s.updateSource)
	s.mux.HandleFunc("DELETE /api/sources/{id}", s.deleteSource)
	s.mux.HandleFunc("POST /api/sources/{id}/crawl", s.triggerCrawl)
	s.mux.HandleFunc("DELETE /api/sources/{id}/crawl", s.stopCrawl)
	s.mux.HandleFunc("GET /api/search", s.searchHandler)
	s.mux.HandleFunc("POST /api/sources/{id}/auth/start", s.authStart)
	s.mux.HandleFunc("GET /api/sources/{id}/auth/poll", s.authPoll)
	s.mux.HandleFunc("PUT /api/sources/{id}/auth/token", s.authSetToken)
	s.mux.HandleFunc("DELETE /api/sources/{id}/auth", s.authRevoke)
	if s.mcpHandler != nil {
		s.mux.Handle("/mcp/sse", s.mcpHandler)
		s.mux.Handle("/mcp/", s.mcpHandler)
	}
	if s.mcpStreamableHandler != nil {
		s.mux.Handle("/mcp/http", s.mcpStreamableHandler)
		s.mux.Handle("/mcp", s.mcpStreamableHandler)
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
			if strings.HasPrefix(path, "/api/") || path == "/mcp" || strings.HasPrefix(path, "/mcp/") {
				got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if subtle.ConstantTimeCompare([]byte(got), []byte(apiKey)) != 1 {
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
		// Alpine.js requires 'unsafe-eval' to evaluate directive expressions.
		// 'unsafe-inline' is kept on style-src for the UI's inline style="..."
		// attributes; it is NOT on script-src — no inline <script> is emitted.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'")

		// CORS: only allow cross-origin requests from loopback addresses.
		// Requests from external sites are denied by omitting the header.
		if origin := r.Header.Get("Origin"); origin != "" {
			if isLoopbackOrigin(origin) {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

// LogAPIKeyStatus logs whether API/MCP bearer-token auth is enabled.
// Call once from main after creating the server.
func LogAPIKeyStatus() {
	if os.Getenv("DOCUMCP_API_KEY") != "" {
		slog.Info("DOCUMCP_API_KEY set — API and MCP endpoints require Authorization: Bearer <key>")
	} else {
		slog.Warn("DOCUMCP_API_KEY not set — API and MCP endpoints are unauthenticated")
	}
}
