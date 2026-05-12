package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mathwro/DocuMcp/internal/db"
	"github.com/mathwro/DocuMcp/internal/search"
)

// sourceInfo is the MCP-facing representation of a documentation source.
// It intentionally omits fields that have no value to an AI client:
//   - Auth: encrypted tokens (security concern)
//   - BaseURL, SpaceKey: adapter-internal identifiers (URL already identifies the source)
//   - CrawlSchedule: internal scheduling detail (LastCrawled gives staleness info)
type sourceInfo struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	URL          string     `json:"url"`
	Repo         string     `json:"repo,omitempty"`
	PageCount    int        `json:"pageCount"`
	CrawlTotal   int        `json:"crawlTotal"`
	LastCrawled  *time.Time `json:"lastCrawled,omitempty"`
	IncludePath  string     `json:"includePath,omitempty"`
	IncludePaths []string   `json:"includePaths,omitempty"`
}

func toSourceInfo(s db.Source) sourceInfo {
	return sourceInfo{
		ID:           s.ID,
		Name:         s.Name,
		Type:         s.Type,
		URL:          s.URL,
		Repo:         s.Repo,
		PageCount:    s.PageCount,
		CrawlTotal:   s.CrawlTotal,
		LastCrawled:  s.LastCrawled,
		IncludePath:  s.IncludePath,
		IncludePaths: s.IncludePaths,
	}
}

// searchResult is the MCP-facing representation of a single search hit.
// It replaces the internal search.Result struct for JSON output:
//   - SourceName replaces SourceID (the name is immediately meaningful to an AI;
//     the integer ID requires a separate list_sources call to interpret).
//   - Score is omitted — results are already ranked best-first; the raw BM25
//     or RRF float is not interpretable and adds noise to the response.
type searchResult struct {
	ResultID   int      `json:"resultId"`
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
		Name: "list_sources",
		Description: "List all configured documentation sources with their names, types, URLs, " +
			"page counts, and last crawl times. Call this first if you do not know what sources " +
			"are available. Source names are required parameters for search_docs and browse_source.",
		InputSchema: objectSchema,
	}, s.handleListSources)

	s.server.AddTool(&sdkmcp.Tool{
		Name: "search_docs",
		Description: "Start here for any documentation question. Searches all indexed sources using " +
			"hybrid BM25 + semantic search and returns compact ranked evidence. If a snippet answers " +
			"the question, answer directly and cite the URL. Only call get_page_excerpt or get_page " +
			"when the snippet is insufficient. Defaults to 5 results with ~300-char snippets. " +
			"Optionally restrict to a single source by name.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query":  {"type": "string", "description": "Search query"},
				"source": {"type": "string", "description": "Optional source name to restrict search"},
				"limit": {"type": "integer", "description": "Maximum results to return (1-10, default 5)"},
				"snippet_chars": {"type": "integer", "description": "Maximum snippet characters per result (80-500, default 300)"}
			},
			"required": ["query"]
		}`),
	}, s.handleSearchDocs)

	s.server.AddTool(&sdkmcp.Tool{
		Name: "browse_source",
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
		Name: "get_page",
		Description: "Retrieve the full content of a single documentation page by URL. Only call " +
			"this when compact search snippets and get_page_excerpt are insufficient. Returns the " +
			"complete page text, which may be large for reference or API pages.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "Page URL"}
			},
			"required": ["url"]
		}`),
	}, s.handleGetPage)

	s.server.AddTool(&sdkmcp.Tool{
		Name: "get_page_excerpt",
		Description: "Retrieve a bounded excerpt from a documentation page by URL. Prefer this over " +
			"get_page when search_docs found a relevant page but the snippet is not enough. If query " +
			"is provided, the excerpt is centered near the first matching term.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {"type": "string", "description": "Page URL"},
				"query": {"type": "string", "description": "Optional focus query for centering the excerpt"},
				"max_chars": {"type": "integer", "description": "Maximum excerpt characters (80-12000, default 4000)"}
			},
			"required": ["url"]
		}`),
	}, s.handleGetPageExcerpt)
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
		Query        string `json:"query"`
		Source       string `json:"source"`
		Limit        int    `json:"limit"`
		SnippetChars int    `json:"snippet_chars"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return toolError(fmt.Sprintf("parse arguments: %v", err))
	}
	if args.Query == "" {
		return toolError("query is required")
	}

	finalLimit := clampInt(args.Limit, 5, 1, 10)
	snippetChars := clampInt(args.SnippetChars, 300, 80, 500)
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
			ResultID:   i + 1,
			URL:        r.URL,
			Title:      r.Title,
			Snippet:    truncateRunes(r.Snippet, snippetChars),
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

// handleGetPageExcerpt handles the get_page_excerpt tool call.
func (s *Server) handleGetPageExcerpt(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	var args struct {
		URL      string `json:"url"`
		Query    string `json:"query"`
		MaxChars int    `json:"max_chars"`
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

	maxChars := clampInt(args.MaxChars, 4000, 80, 12000)
	excerpt := focusedExcerpt(page.Content, args.Query, maxChars)
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: excerpt}},
	}, nil
}

func clampInt(value, def, min, max int) int {
	if value == 0 {
		return def
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func truncateRunes(s string, maxChars int) string {
	if maxChars <= 0 || utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxChars]) + "..."
}

func focusedExcerpt(content, query string, maxChars int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	idx := firstQueryIndex(content, query)
	if idx < 0 {
		return truncateRunes(content, maxChars)
	}
	start := idx - maxChars/3
	return excerptWindow(runes, start, maxChars)
}

func excerptWindow(runes []rune, start, maxChars int) string {
	if start < 0 {
		start = 0
	}
	if start > len(runes)-maxChars {
		start = len(runes) - maxChars
	}
	if start < 0 {
		start = 0
	}
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "..."
	}
	if start+maxChars < len(runes) {
		suffix = "..."
	}
	budget := maxChars - len([]rune(prefix)) - len([]rune(suffix))
	if budget < 0 {
		budget = 0
	}
	end := start + budget
	if end > len(runes) {
		end = len(runes)
	}
	return prefix + string(runes[start:end]) + suffix
}

func firstQueryIndex(content, query string) int {
	query = strings.TrimSpace(query)
	if query == "" {
		return -1
	}
	lowerContent := strings.ToLower(content)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if idx := strings.Index(lowerContent, term); idx >= 0 {
			return utf8.RuneCountInString(content[:idx])
		}
	}
	return -1
}
