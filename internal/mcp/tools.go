package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

// objectSchema is the minimal JSON Schema for a tool that accepts an
// unrestricted JSON object (or no required arguments at all).
var objectSchema = json.RawMessage(`{"type":"object"}`)

// registerTools adds all four MCP tools to the server.
func (s *Server) registerTools() {
	s.server.AddTool(&sdkmcp.Tool{
		Name:        "list_sources",
		Description: "List all configured documentation sources and their crawl status.",
		InputSchema: objectSchema,
	}, s.handleListSources)

	s.server.AddTool(&sdkmcp.Tool{
		Name:        "search_docs",
		Description: "Search documentation using hybrid BM25 + semantic search. Returns up to 10 results.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query":  {"type": "string", "description": "Search query"},
				"source": {"type": "string", "description": "Optional source name to restrict search"}
			},
			"required": ["query"]
		}`),
	}, s.handleSearchDocs)

	s.server.AddTool(&sdkmcp.Tool{
		Name:        "browse_source",
		Description: "Browse a documentation source. Returns top-level sections, or pages within a section.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"source":  {"type": "string", "description": "Source name to browse"},
				"section": {"type": "string", "description": "Optional section name to drill into"}
			},
			"required": ["source"]
		}`),
	}, s.handleBrowseSource)

	s.server.AddTool(&sdkmcp.Tool{
		Name:        "get_page",
		Description: "Retrieve the full content of a documentation page by URL.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "Page URL"}
			},
			"required": ["url"]
		}`),
	}, s.handleGetPage)
}

// toolError returns a CallToolResult that signals a tool-level error (not a
// protocol error) so that the LLM can see and potentially self-correct.
func toolError(msg string) (*sdkmcp.CallToolResult, error) {
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
	}, nil
}

// handleListSources handles the list_sources tool call.
func (s *Server) handleListSources(_ context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	sources, err := s.store.ListSources()
	if err != nil {
		return toolError(fmt.Sprintf("list sources: %v", err))
	}
	data, err := json.Marshal(sources)
	if err != nil {
		return toolError(fmt.Sprintf("marshal sources: %v", err))
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
	}, nil
}

// handleSearchDocs handles the search_docs tool call.
func (s *Server) handleSearchDocs(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var args struct {
		Query  string `json:"query"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return toolError(fmt.Sprintf("parse arguments: %v", err))
	}
	if args.Query == "" {
		return toolError("query is required")
	}

	const finalLimit = 10
	fetchLimit := finalLimit
	if args.Source != "" {
		fetchLimit = finalLimit * 10 // over-fetch so source filter has enough candidates
	}

	ftsResults, err := search.FTS(s.store, args.Query, fetchLimit)
	if err != nil {
		return toolError(fmt.Sprintf("fts search: %v", err))
	}

	semResults, err := search.Semantic(s.store, s.embedder, args.Query, fetchLimit)
	if err != nil {
		// Semantic search failure is non-fatal — log and continue with FTS only.
		slog.Warn("semantic search failed, falling back to FTS only", "err", err)
		semResults = nil
	}

	results := search.MergeRRF(ftsResults, semResults, fetchLimit)

	// Optionally filter by source name.
	if args.Source != "" {
		sourceID, err := s.store.GetSourceIDByName(args.Source)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return toolError(fmt.Sprintf("source %q not found", args.Source))
			}
			return toolError(fmt.Sprintf("lookup source: %v", err))
		}
		filtered := results[:0]
		for _, r := range results {
			if r.SourceID == sourceID {
				filtered = append(filtered, r)
			}
		}
		results = filtered
		if len(results) > finalLimit {
			results = results[:finalLimit]
		}
	}

	data, err := json.Marshal(results)
	if err != nil {
		return toolError(fmt.Sprintf("marshal results: %v", err))
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
	}, nil
}

// handleBrowseSource handles the browse_source tool call.
func (s *Server) handleBrowseSource(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var args struct {
		Source  string `json:"source"`
		Section string `json:"section"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return toolError(fmt.Sprintf("parse arguments: %v", err))
	}
	if args.Source == "" {
		return toolError("source is required")
	}

	sourceID, err := s.store.GetSourceIDByName(args.Source)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return toolError(fmt.Sprintf("source %q not found", args.Source))
		}
		return toolError(fmt.Sprintf("lookup source: %v", err))
	}

	var data []byte
	if args.Section == "" {
		sections, err := search.BrowseTopLevel(s.store, sourceID)
		if err != nil {
			return toolError(fmt.Sprintf("browse top level: %v", err))
		}
		data, err = json.Marshal(sections)
		if err != nil {
			return toolError(fmt.Sprintf("marshal sections: %v", err))
		}
	} else {
		pages, err := search.BrowseSection(s.store, sourceID, args.Section)
		if err != nil {
			return toolError(fmt.Sprintf("browse section: %v", err))
		}
		data, err = json.Marshal(pages)
		if err != nil {
			return toolError(fmt.Sprintf("marshal pages: %v", err))
		}
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
	}, nil
}

// handleGetPage handles the get_page tool call.
func (s *Server) handleGetPage(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return toolError(fmt.Sprintf("parse arguments: %v", err))
	}
	if args.URL == "" {
		return toolError("url is required")
	}

	page, err := s.store.GetPageByURL(args.URL)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return toolError(fmt.Sprintf("page %q not found", args.URL))
		}
		return toolError(fmt.Sprintf("get page: %v", err))
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: page.Content}},
	}, nil
}
