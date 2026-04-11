package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

// sourceInfo is the MCP-facing representation of a documentation source.
// It intentionally omits fields that have no value to an AI client:
//   - Auth: encrypted tokens (security concern)
//   - BaseURL, SpaceKey: adapter-internal identifiers (URL already identifies the source)
//   - CrawlSchedule: internal scheduling detail (LastCrawled gives staleness info)
type sourceInfo struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	URL         string     `json:"url"`
	Repo        string     `json:"repo,omitempty"`
	PageCount   int        `json:"pageCount"`
	CrawlTotal  int        `json:"crawlTotal"`
	LastCrawled *time.Time `json:"lastCrawled,omitempty"`
	IncludePath string     `json:"includePath,omitempty"`
}

func toSourceInfo(s db.Source) sourceInfo {
	return sourceInfo{
		ID:          s.ID,
		Name:        s.Name,
		Type:        s.Type,
		URL:         s.URL,
		Repo:        s.Repo,
		PageCount:   s.PageCount,
		CrawlTotal:  s.CrawlTotal,
		LastCrawled: s.LastCrawled,
		IncludePath: s.IncludePath,
	}
}

// searchResult is the MCP-facing representation of a single search hit.
// It replaces the internal search.Result struct for JSON output:
//   - SourceName replaces SourceID (the name is immediately meaningful to an AI;
//     the integer ID requires a separate list_sources call to interpret).
//   - Score is omitted — results are already ranked best-first; the raw BM25
//     or RRF float is not interpretable and adds noise to the response.
type searchResult struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Snippet    string   `json:"snippet"`
	SourceName string   `json:"sourceName"`
	Path       []string `json:"path"`
}

// objectSchema is the minimal JSON Schema for a tool that accepts an
// unrestricted JSON object (or no required arguments at all).
var objectSchema = json.RawMessage(`{"type":"object"}`)

// registerTools adds all four MCP tools to the server.
func (s *Server) registerTools() {
	s.server.AddTool(&sdkmcp.Tool{
		Name:        "list_sources",
		Description: "List all configured documentation sources with their names, types, URLs, " +
			"page counts, and last crawl times. Call this first if you do not know what sources " +
			"are available. Source names are required parameters for search_docs and browse_source.",
		InputSchema: objectSchema,
	}, s.handleListSources)

	s.server.AddTool(&sdkmcp.Tool{
		Name:        "search_docs",
		Description: "Start here for any documentation question. Searches all indexed sources using " +
			"hybrid BM25 + semantic search and returns up to 10 results ranked by relevance. Each " +
			"result includes the source name, section path, and a short excerpt (~200 chars) centred " +
			"on the matched terms. If an excerpt confirms the page is relevant, call get_page with " +
			"that URL for the full content. Optionally restrict to a single source by name.",
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
		Description: "Explore the structure of a documentation source. Without section: returns all " +
			"top-level sections with page counts — use this to understand what a source contains. " +
			"With section: returns up to 50 pages (URL + title) in that section. Prefer search_docs " +
			"when you have a specific question; use browse_source when you need to navigate the " +
			"documentation hierarchy or when search results are insufficient.",
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
		Description: "Retrieve the full content of a single documentation page by URL. Only call " +
			"this after confirming relevance — use search_docs first to find candidate URLs and " +
			"read their excerpts. Returns the complete page text, which may be large for reference " +
			"or API pages.",
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
	infos := make([]sourceInfo, len(sources))
	for i, src := range sources {
		infos[i] = toSourceInfo(src)
	}
	data, err := json.Marshal(infos)
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

	if len(results) == 0 {
		data, _ := json.Marshal(make([]searchResult, 0))
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
		}, nil
	}

	// Build a source ID → name map so each result can carry a human-readable name.
	allSources, err := s.store.ListSources()
	if err != nil {
		return toolError(fmt.Sprintf("lookup sources: %v", err))
	}
	sourceNames := make(map[int64]string, len(allSources))
	for _, src := range allSources {
		sourceNames[src.ID] = src.Name
	}

	out := make([]searchResult, len(results))
	for i, r := range results {
		out[i] = searchResult{
			URL:        r.URL,
			Title:      r.Title,
			Snippet:    r.Snippet,
			SourceName: sourceNames[r.SourceID],
			Path:       r.Path,
		}
	}
	data, err := json.Marshal(out)
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
