# Repository Guidelines

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

SQLite uses FTS5 plus sqlite-vec; `bm25()` scores sort best with `ORDER BY score ASC`. New DB columns need schema updates and idempotent `ALTER TABLE` migrations in `Open()`. When adding fields to `db.Source`, update inserts, selects, scans, and `sourceToConfig()` in `crawler.go`. Adapter `Crawl` returns `(int, <-chan db.Page, error)`, where `int` is total URL count or `0` if unknown. Keep HTTP source access routed through `internal/httpsafe`.

## Commit & Pull Request Guidelines

Recent commits use Conventional Commits, for example `feat(bench): add JSON report writer` and `fix(bench): parse flags once`. Keep branches focused and target `main`. PRs should explain why, summarize changes, link issues, and list test commands. Include screenshots only for UI changes.

## Security & Configuration Tips

Do not commit `.env`, real tokens, or local `config.yaml`. `DOCUMCP_CONFIG` enables strict config loading; unset uses defaults. `DOCUMCP_SECRET_KEY` protects stored tokens; unset keys are ephemeral. `DOCUMCP_API_KEY` protects `/api/*` and `/mcp/*`.
