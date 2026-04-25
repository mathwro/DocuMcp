# Development

## Requirements

- Go 1.26+ with CGo enabled
- GCC / `libsqlite3-dev` (for `go-sqlite3`)

## Build & Test

```bash
make build   # CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
make test    # CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
make lint    # requires golangci-lint
```

> Both `CGO_ENABLED=1` and `-tags sqlite_fts5` are required — SQLite FTS5 and sqlite-vec use CGo.

## Project Layout

```
cmd/documcp/          # main binary
internal/
  adapter/            # source adapters (web, github, githubrepo, azuredevops)
  api/                # REST API handlers and server
  auth/               # Microsoft device code flow, encrypted token store
  config/             # YAML config + file watcher
  crawler/            # crawl orchestrator + cron scheduler
  db/                 # SQLite schema, CRUD, FTS5, tokens
  embed/              # ONNX embedding model wrapper
  mcp/                # MCP server + 4 tool handlers
  search/             # FTS5, semantic search, RRF, browse
web/static/           # embedded HTML/JS/CSS (Alpine.js, dark theme)
models/               # ONNX model (downloaded at Docker build time)
```

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the development loop, code style, and what is likely to be accepted or pushed back on.
