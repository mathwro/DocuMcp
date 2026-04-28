// Package mcp implements the MCP server with four tool handlers for DocuMcp.
package mcp

import (
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/embed"
)

// Server wraps the go-sdk MCP server and holds references to the data layer.
type Server struct {
	store    *db.Store
	embedder *embed.Embedder
	server   *sdkmcp.Server
}

// NewServer creates a new MCP server with all four tools registered.
func NewServer(store *db.Store, embedder *embed.Embedder) *Server {
	s := &Server{store: store, embedder: embedder}
	s.server = sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "DocuMcp",
		Version: "1.0.0",
	}, nil)
	s.registerTools()
	return s
}

// Handler returns an HTTP handler that serves the MCP protocol over SSE.
func (s *Server) Handler() http.Handler {
	return sdkmcp.NewSSEHandler(func(r *http.Request) *sdkmcp.Server {
		return s.server
	}, nil)
}

// StreamableHTTPHandler returns an HTTP handler for clients that use the
// streamable HTTP MCP transport.
func (s *Server) StreamableHTTPHandler() http.Handler {
	return sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return s.server
	}, nil)
}

// SDKServer exposes the underlying go-sdk server for testing.
func (s *Server) SDKServer() *sdkmcp.Server {
	return s.server
}
