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

## Implementation Plan
`docs/plans/2026-02-27-documcp-implementation.md` — 27 tasks, progress tracker at top.

**Completed:** Tasks 1–16 (foundation, search, adapters, auth)
**Next:** Task 17 — Crawl orchestrator (`internal/crawler/`)

To continue: invoke `superpowers:subagent-driven-development` and pick up at Task 6.

## Architecture Decisions
- Single Go binary — MCP server + Web UI + REST API + crawlers in one process
- SQLite with FTS5 (keyword) + sqlite-vec (vector) for hybrid search
- `all-MiniLM-L6-v2` ONNX model bundled in Docker — zero user setup for semantic search
- Auth via device code flows only — no app registrations required from users
- Azure CLI client ID `04b07795-8ddb-461a-bbee-02f9e1bf7b46` for Microsoft flows
- Config file is source of truth; Web UI reads/writes it; watcher reloads without restart
- Tokens stored AES-256-GCM encrypted in SQLite, never in config file
