# Contributing to DocuMcp

Thanks for taking the time to contribute! This document covers the
development loop and what I look for in a pull request.

## Development loop

### Requirements

- Go 1.26+ with CGo enabled
- GCC and `libsqlite3-dev` (or the equivalent on your distro) — the
  SQLite FTS5 and sqlite-vec bindings both require CGo
- `podman` or `docker` if you want to build the container image

### Build and test

```bash
make build   # CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
make test    # CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

Both the `CGO_ENABLED=1` and `-tags sqlite_fts5` flags are required for
every build and test command. If you forget either, the binary will panic
on first query.

### Run locally against your own config

```bash
make init              # writes config.yaml and .env with a generated key
$EDITOR config.yaml    # add a source or two
DOCUMCP_CONFIG=./config.yaml \
DOCUMCP_SECRET_KEY="$(grep DOCUMCP_SECRET_KEY .env | cut -d= -f2)" \
go run -tags sqlite_fts5 ./cmd/documcp
```

The server binds to `127.0.0.1:8080` by default. Open
<http://127.0.0.1:8080> to manage sources and drive searches.

## Submitting a pull request

Before opening a PR, please:

- Base the branch on `main` and keep it focused on a single change.
- Run `gofmt -w .`, `go vet -tags sqlite_fts5 ./...`, and
  `go test -tags sqlite_fts5 -race ./...` — CI enforces all three.
- Add tests for new behavior. Integration tests that exercise HTTP
  handlers should use `httptest` against the in-memory SQLite store, not
  `httptest.NewServer` for source adapters — the web adapter's SSRF
  check blocks loopback IPs, so use stub adapters in adapter tests.
- Write PR descriptions that explain *why* the change is needed and what
  you tested. A bullet summary and a manual test plan are plenty.

## Code style

- Wrap errors: `fmt.Errorf("context: %w", err)`. Never return a raw
  error from a package boundary.
- Return `db.ErrNotFound` for missing rows; callers use `errors.Is`.
- Return empty slices (`make([]T, 0)`), not `nil`, from list operations.
- Keep comments terse — explain *why*, not *what*. Remove dead code
  rather than commenting it out.
- No `// Added for X` / `// Used by Y` comments; the code speaks for
  itself and those comments rot.

## Scope I am likely to accept

- Additional source adapters (GitLab Wiki, Confluence, Notion, etc.)
- Incremental crawling (ETags, Last-Modified)
- Bug fixes and regression tests
- Security hardening
- Documentation improvements

## Scope I am likely to push back on

- Multi-tenant features — DocuMcp is a single-user local service.
- UI overhauls or framework swaps. The current Alpine.js UI is
  deliberately minimal.
- New runtime dependencies (a second database, a message broker, a
  second embedding model). The single-binary + single-SQLite shape is a
  product decision, not an accident.

## License

By contributing, you agree that your contribution is licensed under
GPL-3.0, matching the rest of the project. See [LICENSE](LICENSE).
