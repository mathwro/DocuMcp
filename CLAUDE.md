# DocuMcp — Claude Code Instructions

## Build & Test
```bash
# Always use both flags — CGo and FTS5 are required
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...

# Makefile wraps these correctly
make build
make test
```

## Project Structure
```
cmd/documcp/        # main binary entry point
internal/config/    # YAML config + fsnotify file watcher
internal/db/        # SQLite schema, CRUD, FTS5, tokens
internal/search/    # FTS5 search, semantic search, RRF, browse
internal/embed/     # ONNX embedding model wrapper (hugot)
internal/adapter/   # Adapter interface + web/github/azuredevops impls
internal/auth/      # Device code flows (Microsoft, GitHub), token store
internal/crawler/   # Crawl orchestrator + cron scheduler
internal/mcp/       # MCP server + 4 tool handlers
internal/api/       # REST API for Web UI
web/static/         # Embedded HTML/JS/CSS (dark theme, Alpine.js)
models/             # ONNX model (downloaded at Docker build time)
docs/plans/         # Design doc + implementation plan
```

## Key Patterns
- **Error wrapping:** always `fmt.Errorf("context: %w", err)` — never return raw errors
- **Not found:** use `db.ErrNotFound` sentinel; callers check with `errors.Is(err, db.ErrNotFound)`
- **Empty slices:** return `make([]T, 0)` not `nil` for list operations
- **Tests:** always check setup errors with `t.Fatalf`, not `_`
- **FTS5 bm25:** returns negative values — `ORDER BY score ASC` = best first
- **DB migrations:** add new columns via `_, _ = db.Exec("ALTER TABLE ... ADD COLUMN ...")` in `Open()` after the schema exec — errors silently ignored (idempotent)
- **Adapter interface:** `Crawl` returns `(int, <-chan db.Page, error)` — first value is total URL count (0 if unknown)
- **sourceToConfig:** when adding fields to `db.Source`, always add them to `sourceToConfig` in `crawler.go` too
- **Alpine.js:** load `app.js` before `alpinejs.min.js` (defer order matters); CSP includes `unsafe-eval` for Alpine expression evaluation
- **Web adapter SSRF:** `isAllowedHost()` blocks loopback/private IPs — use stub adapters in tests, not `httptest.NewServer`
- **HTML extraction:** `skipTags` in `extract.go` excludes script/style/noscript/iframe/nav/footer/header/aside; title prefers h1, falls back to `<title>` tag (keeps longer side of ` | ` split)
- **Search snippets:** HTML tags stripped via regex in `scanResults` before returning results

## Architecture Decisions
- Single Go binary — MCP server + Web UI + REST API + crawlers in one process
- SQLite with FTS5 (keyword) + sqlite-vec (vector) for hybrid search
- `all-MiniLM-L6-v2` ONNX model bundled in container image — zero user setup for semantic search
- Container support: Makefile auto-detects podman vs docker (prefers podman)
- Auth via device code flows only — no app registrations required from users
- Azure CLI client ID `04b07795-8ddb-461a-bbee-02f9e1bf7b46` for Microsoft flows
- Config file is source of truth; Web UI reads/writes it; watcher reloads without restart
- Tokens stored AES-256-GCM encrypted in SQLite, never in config file
- Crawl progress tracked server-side (`crawlingIDs map[int64]bool` in API server); `CrawlTotal` stored in DB at crawl start, `PageCount` reset to 0 and flushed every 10 pages
- `include_path` field on web sources restricts crawling to a URL prefix; validated same-origin; forwarded through `sourceToConfig`
- Web adapter discovers URLs synchronously before goroutine so total count is known upfront
- Alpine.js vendored at `web/static/alpinejs.min.js` (no CDN); CSP: `script-src 'self' 'unsafe-inline' 'unsafe-eval'`

## Status
All 27 original tasks complete. Post-launch improvements committed to `main`:
- Security fixes (PR #4), code quality fixes (PR #5)
- Polite web crawler: 500ms delay, User-Agent, 429/Retry-After handling
- Sitemap discovery: tries `<src>/sitemap.xml` then `<origin>/sitemap.xml`
- Live crawl progress UI: `N / Total pages` badge with 2s polling
- Search UI: clickable result titles, HTML-stripped snippets, page title from `<title>` tag
- `include_path` field for web sources (path-prefix filtering)
