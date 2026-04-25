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
internal/adapter/   # Adapter interface + web/github/githubrepo/azuredevops impls
internal/auth/      # Token store (AES-256-GCM); GitHub PAT save/revoke + Azure DevOps device code flow
internal/crawler/   # Crawl orchestrator + cron scheduler
internal/mcp/       # MCP server + 4 tool handlers
internal/api/       # REST API for Web UI
internal/httpsafe/  # SSRF-safe HTTP client (loopback/private-IP blocking)
internal/testutil/  # Shared test helpers
web/static/         # Embedded HTML/JS/CSS (dark theme, Alpine.js)
docs/               # User-facing docs (install, configuration, sources, mcp-clients, troubleshooting, development)
docs/plans/         # Per-feature design + implementation plans (one pair per major change)
```

> The ONNX model lives at `/app/models/all-MiniLM-L6-v2` inside the container only — it is downloaded at image-build time and not in the repo.

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
- Server binds to `127.0.0.1:<port>` by default — set `DOCUMCP_BIND_ADDR=0.0.0.0:<port>` to expose to LAN. Docker image overrides to `0.0.0.0:8080` so port mapping works
- Auth: Azure DevOps uses Microsoft device code flow (Azure CLI client ID `04b07795-8ddb-461a-bbee-02f9e1bf7b46`); GitHub (wiki + repo) uses user-supplied fine-grained PATs via `PUT /api/sources/{id}/auth/token`
- Public GitHub repos/wikis crawl without a token; PAT only required for private resources
- `config.yaml` is **optional** — when missing, built-in defaults apply (`port: 8080`, `data_dir: /app/data`, no declarative sources). When present, the watcher reloads it without restart. Sources added via the Web UI persist in SQLite (`db.Source`), NOT written back to YAML — YAML is operator-managed declarative seeding only
- `DOCUMCP_CONFIG` set → `config.Load` (strict, missing file is fatal). Unset → `config.LoadOrDefault` (lenient, missing file falls back to defaults)
- Tokens stored AES-256-GCM encrypted in SQLite; key from `DOCUMCP_SECRET_KEY` (hex or base64, 32 bytes). Unset → ephemeral key, tokens lost on restart
- Crawl progress tracked server-side (`crawlingIDs map[int64]bool` in API server); `CrawlTotal` stored in DB at crawl start, `PageCount` reset to 0 and flushed every 10 pages
- `include_path` field: on `web` sources restricts crawling to a URL prefix (validated same-origin); on `github_repo` sources restricts indexing to a subfolder (`..` segments rejected)
- `github_repo` adapter streams `/repos/{owner}/{repo}/tarball/{branch}` via `archive/tar` + `compress/gzip`; indexes `.md`/`.mdx`/`.txt`; 5 MiB per-file cap; one 429 retry honoring `Retry-After`
- Web adapter discovers URLs synchronously before goroutine so total count is known upfront
- Alpine.js vendored at `web/static/alpinejs.min.js` (no CDN); CSP: `script-src 'self' 'unsafe-inline' 'unsafe-eval'`

## Environment Variables
| Var | Purpose |
|---|---|
| `DOCUMCP_CONFIG` | Path to config file. Set → strict (missing file is fatal). Unset → defaults to `/app/config.yaml`, lenient |
| `DOCUMCP_SECRET_KEY` | 32-byte hex/base64 key for token encryption. Unset → ephemeral |
| `DOCUMCP_API_KEY` | Bearer token for `/api/*` and `/mcp/*`. Unset → open (warns at startup) |
| `DOCUMCP_BIND_ADDR` | Listen address. Unset → `127.0.0.1:<port>`. Container image sets `0.0.0.0:8080` |
| `DOCUMCP_MODEL_PATH` | ONNX model directory. Default `/app/models/all-MiniLM-L6-v2` |

Detailed reference: `docs/configuration.md`. CI runs `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./...` (see `.github/workflows/ci.yml`).

## History
The project shipped its initial 27-task plan and several follow-ups; see `git log` for the full changelog and `docs/plans/` for per-feature design + implementation pairs. This file documents the current contract, not the path that got us here.
