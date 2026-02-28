package api

import (
	"net/http"

	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
)

// Server is the REST API server for the Web UI.
type Server struct {
	store      *db.Store
	crawler    *crawler.Crawler
	mcpHandler http.Handler
	mux        *http.ServeMux
}

// NewServer creates a new API server wired to the given store, crawler, and MCP handler.
// Pass nil for mcpHandler and/or crawler when not needed (e.g. in tests).
func NewServer(store *db.Store, c *crawler.Crawler, mcpHandler http.Handler) *Server {
	s := &Server{store: store, crawler: c, mcpHandler: mcpHandler, mux: http.NewServeMux()}
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
	if s.mcpHandler != nil {
		s.mux.Handle("/mcp/", s.mcpHandler)
	}
	// Static files — will be added in Task 22
}
