# Repository Guidelines

## Branch Strategy

Before starting any new work, including docs, fixes, refactors, features, or commits, confirm with the user whether to stay on the current branch or create a dedicated branch. Always ask, even for small changes, and ask before any code edit, file write, or commit. The default when unsure is a focused branch named for the goal, such as `feat/...`, `fix/...`, or `docs/...`. If the user chooses the current branch, proceed there.

## Project Structure & Module Organization

DocuMcp is a single Go 1.26 binary for the MCP endpoint, REST API, web UI, and crawlers. `cmd/documcp/` is the server; `cmd/bench/` is benchmark tooling. Core packages live in `internal/`: `adapter/`, `api/`, `auth/`, `config/`, `crawler/`, `db/`, `embed/`, `httpsafe/`, `mcp/`, `search/`, and `testutil/`. Static Alpine.js assets are in `web/static/` and embedded by `web/embed.go`. User docs are in `docs/`; plans are in `docs/plans/`. Tests are colocated as `*_test.go`.

## Build, Test, and Development Commands

- `make init`: create `.env` with `DOCUMCP_SECRET_KEY`.
- `make build`: build `bin/documcp`.
- `make bench`: build `bin/bench`.
- `make test`: run Go tests.
- `make lint`: run `golangci-lint run`.
- `make docker`: build `documcp:local`.
- `make run`: start compose with Podman or Docker.

For direct Go commands, always use both flags: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./...`; SQLite FTS5 and sqlite-vec require CGo.

## Coding Style & Naming Conventions

Run `gofmt -w .` before submitting. Use short lowercase package names, `CamelCase` exports, and `camelCase` internals. Wrap package-boundary errors, for example `fmt.Errorf("load source: %w", err)`. Use `db.ErrNotFound` for missing rows and check with `errors.Is`. Return empty slices, not `nil`. Keep comments focused on why.

## Testing Guidelines

Use Go's standard `testing` package. Name tests `TestXxx` and check setup errors with `t.Fatalf`. Before a PR, run `gofmt -w .`, `CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...`, and `CGO_ENABLED=1 go test -tags sqlite_fts5 -race ./...`. Adapter tests should use stubs or resolver overrides, not `httptest.NewServer`, because SSRF protection blocks loopback/private IPs.

## Architecture & Agent-Specific Notes

DocuMcp is one process: MCP server, REST API, web UI, crawlers, SQLite FTS5 keyword search, sqlite-vec vector search, and the ONNX embedding wrapper. The `all-MiniLM-L6-v2` model is bundled in the container at `/app/models/all-MiniLM-L6-v2`, downloaded during image build, and not committed to the repository. The Makefile auto-detects container runtime and prefers Podman over Docker.

Server binding defaults to `127.0.0.1:<port>`; set `DOCUMCP_BIND_ADDR=0.0.0.0:<port>` for LAN exposure. The container image overrides this to `0.0.0.0:8080` so port mapping works. `config.yaml` is optional: unset `DOCUMCP_CONFIG` uses defaults leniently, while setting `DOCUMCP_CONFIG` makes a missing file fatal. The file watcher reloads YAML without restart. Sources added through the Web UI persist in SQLite, not back to YAML.

Tokens are encrypted in SQLite with AES-256-GCM. `DOCUMCP_SECRET_KEY` must be a 32-byte hex or base64 key for stable encrypted token storage; if unset, an ephemeral key is generated and saved tokens are unusable after restart. Azure DevOps auth uses Microsoft device code flow with the Azure CLI client ID `04b07795-8ddb-461a-bbee-02f9e1bf7b46`. GitHub repos and wikis use user-supplied fine-grained PATs for private access; public GitHub repos and wikis work unauthenticated.

SQLite uses FTS5 plus sqlite-vec; `bm25()` returns negative values, so `ORDER BY score ASC` sorts most relevant first. New DB columns need schema updates and idempotent `ALTER TABLE` migrations in `Open()`. When adding fields to `db.Source`, update inserts, selects, scans, and `sourceToConfig()` in `crawler.go`. Adapter `Crawl` returns `(int, <-chan db.Page, error)`, where `int` is total URL count or `0` if unknown. Keep HTTP source access routed through `internal/httpsafe`.

Crawl progress is tracked in the API server with `crawlingIDs map[int64]bool`; `CrawlTotal` is set at crawl start, `PageCount` is reset to 0, and page count updates flush every 10 pages. The `include_path` field restricts web sources to a same-origin URL prefix, and restricts `github_repo` sources to a subfolder with `..` segments rejected. The `github_repo` adapter streams repository tarballs, indexes `.md`, `.mdx`, and `.txt`, enforces a 5 MiB per-file cap, and retries one 429 response using `Retry-After`. The web adapter discovers URLs synchronously before starting its crawl goroutine, so total count is known upfront.

Alpine.js is vendored at `web/static/alpinejs.min.js` with no CDN. Load `app.js` deferred before `alpinejs.min.js`; the order matters because Alpine needs `app()` to exist. CSP includes `unsafe-eval` for Alpine expressions. HTML extraction excludes `script`, `style`, `noscript`, `iframe`, `nav`, `footer`, `header`, and `aside`; titles prefer `h1`, then `<title>`, keeping the longer side of a ` | ` split. Search snippets strip HTML tags via regex in `scanResults`.

## Environment Variables

| Var | Purpose |
| --- | --- |
| `DOCUMCP_CONFIG` | Path to config file. Set means strict loading and missing file is fatal; unset uses `/app/config.yaml` leniently with defaults. |
| `DOCUMCP_SECRET_KEY` | 32-byte hex/base64 key for token encryption. Unset means ephemeral and stored tokens are lost on restart. |
| `DOCUMCP_API_KEY` | Bearer token for `/api/*` and `/mcp/*`. Unset leaves those endpoints open and logs a startup warning. |
| `DOCUMCP_BIND_ADDR` | Listen address. Unset means `127.0.0.1:<port>`; the container image sets `0.0.0.0:8080`. |
| `DOCUMCP_MODEL_PATH` | ONNX model directory. Default is `/app/models/all-MiniLM-L6-v2`. |

## Commit & Pull Request Guidelines

Recent commits use Conventional Commits, for example `feat(bench): add JSON report writer` and `fix(bench): parse flags once`. Keep branches focused and target `main`. PRs should explain why, summarize changes, link issues, and list test commands. Include screenshots only for UI changes.

## Security & Configuration Tips

Do not commit `.env`, real tokens, or local `config.yaml`. `DOCUMCP_CONFIG` enables strict config loading; unset uses defaults. `DOCUMCP_SECRET_KEY` protects stored tokens; unset keys are ephemeral. `DOCUMCP_API_KEY` protects `/api/*` and `/mcp/*`.
