package api

import (
	"net/http"
	"sync"

	"github.com/documcp/documcp/internal/auth"
	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
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
	tokenStore   *auth.TokenStore
	pendingFlows map[int64]*pendingFlow
	flowMu       sync.Mutex
}

// NewServer creates a new API server wired to the given store, crawler, and MCP handler.
// Pass nil for mcpHandler and/or crawler when not needed (e.g. in tests).
func NewServer(store *db.Store, c *crawler.Crawler, mcpHandler http.Handler) *Server {
	// Use a zeroed 32-byte key for now; can be made configurable via env var later.
	key := make([]byte, 32)
	s := &Server{
		store:        store,
		crawler:      c,
		mcpHandler:   mcpHandler,
		mux:          http.NewServeMux(),
		tokenStore:   auth.NewTokenStore(store, key),
		pendingFlows: make(map[int64]*pendingFlow),
	}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
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
	// Static files — will be added in Task 22
}
