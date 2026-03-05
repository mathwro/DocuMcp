# DocuMcp — Copilot Instructions

## What This Is
A self-hosted MCP (Model Context Protocol) server that indexes documentation from multiple sources and makes it searchable by AI coding assistants. Single Go binary: MCP server + REST API + Web UI + crawlers.

## Build & Test
```bash
# CGO_ENABLED=1 and -tags sqlite_fts5 are ALWAYS required
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...

make build   # wraps the above
make test
make docker  # builds podman/docker image as documcp:local
```

## Project Layout
```
cmd/documcp/        # main binary — wires everything together
internal/config/    # YAML config + fsnotify file watcher
internal/db/        # SQLite schema, CRUD, FTS5, tokens, migrations
internal/search/    # FTS5 + semantic search + RRF + browse
internal/embed/     # ONNX embedding inference (hugot)
internal/adapter/   # Adapter interface + web/github/azuredevops impls
internal/auth/      # Device code flows (Microsoft, GitHub), encrypted token store
internal/crawler/   # Crawl orchestrator + cron scheduler
internal/mcp/       # MCP server — 4 tools via go-sdk SSE
internal/api/       # REST API handlers + static file serving
web/static/         # Embedded HTML/JS/CSS — Alpine.js, dark theme
models/             # ONNX model dir (downloaded at Docker build time)
docs/plans/         # Design docs and implementation plans
```

## Key Coding Patterns

### Errors
- Always wrap: `fmt.Errorf("context: %w", err)`
- Not found: return `db.ErrNotFound`; callers use `errors.Is(err, db.ErrNotFound)`

### DB / Schema
- New columns: add to `CREATE TABLE` in `schema.go` AND add an idempotent migration in `Open()`:
  ```go
  _, _ = db.Exec(`ALTER TABLE sources ADD COLUMN foo TEXT NOT NULL DEFAULT ''`)
  ```
- When adding a field to `db.Source`, update: struct, INSERT, both SELECTs, both Scans, and `sourceToConfig()` in `crawler.go`

### Adapter Interface
```go
Crawl(ctx context.Context, source config.SourceConfig, sourceID int64) (int, <-chan db.Page, error)
// int = total URL count (0 if unknown); channel closed when done
```

### Web Adapter
- SSRF protection: `isAllowedHost()` blocks loopback/private IPs — never use `httptest.NewServer` in tests; use stub adapters instead
- `filterURL(u, base, filterPath)` helper encapsulates origin + path-prefix + SSRF checks
- `include_path` field: if set, overrides `base.Path` as the URL prefix filter; must share same origin
- Sitemap discovery: tries `<src>/sitemap.xml` then `<origin>/sitemap.xml`
- Polite crawling: 500ms delay between pages, User-Agent header, 429/Retry-After backoff

### HTML Extraction (`internal/adapter/web/extract.go`)
- `skipTags`: script, style, noscript, iframe, nav, footer, header, aside
- Title: prefers `<h1>`, falls back to `<title>` tag (keeps the longer part of ` | ` / ` - ` splits)

### Search
- FTS5 `bm25()` returns negative values — `ORDER BY score ASC` = best first
- Snippets: HTML tags stripped via regex in `scanResults` before returning

### Web UI
- Alpine.js vendored at `web/static/alpinejs.min.js` (no CDN dependency)
- Load order: `app.js` must be deferred before `alpinejs.min.js`
- CSP: `script-src 'self' 'unsafe-inline' 'unsafe-eval'` (Alpine needs `unsafe-eval`)
- `db.Source` JSON uses PascalCase (no struct tags); auth API uses snake_case

### Crawl Progress
- `CrawlTotal` stored in DB at crawl start (from URL count discovered before goroutine)
- `PageCount` reset to 0 at start, flushed every 10 pages incrementally
- `crawlingIDs map[int64]bool` (mutex-protected) in API server tracks active crawls
- UI polls `/api/sources` every 2s when any source has `Crawling: true`

## Architecture
- **Storage:** SQLite with FTS5 + sqlite-vec; `all-MiniLM-L6-v2` ONNX model bundled in image
- **Auth:** Device code flows only — no app registrations needed
  - Microsoft: Azure CLI client ID `04b07795-8ddb-461a-bbee-02f9e1bf7b46`
  - GitHub: standard device flow
- **Tokens:** AES-256-GCM encrypted in SQLite; key from `DOCUMCP_SECRET_KEY` env var
- **MCP:** `github.com/modelcontextprotocol/go-sdk` v0.8.0, SSE transport at `/mcp/`
- **Container:** multi-stage Dockerfile, non-root user `documcp` (uid 1001), podman-compatible

## Source Types
| Type | Auth | Key fields |
|---|---|---|
| `web` | none / basic | `url`, `include_path` (optional path-prefix filter) |
| `github_wiki` | GitHub device code | `repo` (owner/repo) |
| `azure_devops` | Microsoft device code | `url`, `base_url` |
