# GitHub Repo Source Adapter — Design

**Date:** 2026-04-16
**Status:** Approved, awaiting implementation plan

## Summary

Add a new source type `github_repo` that indexes documentation files (`.md`, `.mdx`, `.txt`) from any GitHub repository, optionally scoped to a subfolder. Fetches the whole repo as a single tarball in one HTTP request, streams it through `archive/tar`, and emits `db.Page` entries preserving the repo's folder hierarchy for hierarchical browsing.

This is distinct from the existing `github_wiki` adapter, which indexes the separate wiki repo. Users commonly keep docs inside a repo's `docs/` folder — this adapter covers that case.

## Goals

- Index Markdown and text docs from any public or private GitHub repo.
- Support scoping to a subfolder (e.g., `docs/`) via the existing `include_path` config field.
- Reuse the existing GitHub device-code auth flow and token store. No new auth code.
- Minimize API rate-limit pressure: one HTTP request per crawl regardless of repo size.
- Preserve folder structure in `db.Page.Path` so `browse_source` reflects the repo tree.

## Non-goals (v1)

- Incremental crawling (commit-SHA tracking, conditional requests). No existing adapter does this yet; out of scope.
- Auto-detection of the repo's default branch. User specifies `branch` or accepts the `main` default.
- Hosts other than github.com. GitLab, Bitbucket, self-hosted, Azure Repos are deferred.
- File formats beyond `.md`/`.mdx`/`.txt`. No `.rst`, `.adoc`, source code.
- Rendering or parsing Markdown. Content is stored raw, as with the wiki adapter.
- Multi-branch indexing per source.

## Architecture

### Package layout

New package: `internal/adapter/githubrepo/` containing:

- `githubrepo.go` — adapter struct, `init()` registration, `Crawl`, `NeedsAuth`.
- `githubrepo_test.go` — tests using a local `httptest.Server` and in-memory tarball fixtures.

The existing `internal/adapter/github/` package (GitHub Wiki) is untouched.

### Adapter struct

```go
type GitHubRepoAdapter struct{ baseURL string }

func NewAdapter(baseURL string) *GitHubRepoAdapter { ... }

func init() {
    adapter.Register("github_repo", NewAdapter("https://api.github.com"))
}
```

The `baseURL` field enables test injection of a mock server, mirroring the wiki adapter.

### `NeedsAuth`

Returns `true` always. Public repos crawl without a token (GitHub allows anonymous tarball downloads at 60 req/hr); private repos require a token. Returning `true` surfaces the auth-setup UI to users regardless, matching the wiki adapter's convention.

## Config schema

New source type `github_repo` consumes the following fields from `config.SourceConfig`:

| Field | Type | Required | Default | Purpose |
|---|---|---|---|---|
| `type` | string | yes | — | Literal `"github_repo"`. |
| `repo` | string | yes | — | `owner/name`. Same field the `github_wiki` adapter uses. |
| `branch` | string | no | `"main"` | Git ref to crawl. User-overridable; no auto-detection. |
| `include_path` | string | no | `""` | Subfolder prefix (e.g., `docs/`). Empty = whole repo. Reuses the existing `include_path` field from `SourceConfig` (today used by the web adapter). |

### Fields added by this work

`branch` is new — it does not currently exist on `config.SourceConfig` or `db.Source`. Adding it requires:

1. **`config.SourceConfig`** (`internal/config/config.go`) — add `Branch string \`yaml:"branch,omitempty"\``.
2. **`db.Source`** (`internal/db/db.go`) — add `Branch string`.
3. **Schema** (`internal/db/schema.go`) — add `branch TEXT NOT NULL DEFAULT ''` to the `sources` `CREATE TABLE`.
4. **Idempotent migration** in `Open()` — `_, _ = db.Exec("ALTER TABLE sources ADD COLUMN branch TEXT NOT NULL DEFAULT ''")` per the project's migration convention.
5. **CRUD** — update the `INSERT`, both `SELECT`s, and both `Scan` call sites to include `branch`.
6. **`sourceToConfig`** in `internal/crawler/crawler.go` — forward `Branch` from `db.Source` to `config.SourceConfig`, applying the `"main"` default when empty.

`include_path` already exists on both `config.SourceConfig` and `db.Source`; no schema change needed for that field.

### Config validation

- `repo` must match `/^[^/]+/[^/]+$/` (owner/name, no slashes inside segments).
- `include_path`, if set, is validated in the adapter's `Crawl` entry point before any HTTP request: the raw value is run through `path.Clean` (after trimming any leading `/`); if the cleaned value is `..`, starts with `../`, or contains `/../`, `Crawl` returns a wrapped error. Matches the existing web-adapter pattern for `include_path` validation.
- `branch`, if set, is used verbatim in the URL path after `url.PathEscape`.

## Data flow

1. **Fetch tarball.** `GET {baseURL}/repos/{repo}/tarball/{branch}`. Headers:
   - `Accept: application/vnd.github+json`
   - `Authorization: Bearer {token}` when `src.Token != ""`
   - `User-Agent: documcp` (matches other outbound requests)
2. **Follow redirect.** GitHub responds `302` to a signed `codeload.github.com` URL. Go's default `http.Client` follows redirects and correctly drops `Authorization` on cross-host hops, so the signed URL carries its own auth. This is verified in tests.
3. **Stream decode.** `gzip.NewReader(resp.Body)` → `tar.NewReader(gz)`. No temporary files, no full-body buffering.
4. **Iterate tar entries.** For each entry:
   1. Skip if `hdr.Typeflag != tar.TypeReg`.
   2. Strip the leading `{owner}-{repo}-{sha}/` prefix that GitHub always prepends.
   3. If `include_path` is set and the stripped path does not start with it, skip.
   4. If the file extension is not in `{.md, .mdx, .txt}`, skip.
   5. If `hdr.Size > 5 * 1024 * 1024` (5 MiB), `slog.Warn("github_repo: file too large, skipping", "path", ...)` and skip.
   6. Read content via `io.ReadAll(io.LimitReader(tr, 5 MiB))`.
   7. Build and emit a `db.Page` (see below).
5. **Completion.** Channel closes on goroutine exit (`defer close(ch)`), on `ctx.Done()`, or on mid-stream decode error.
6. **Total-count return.** `Crawl` returns `(0, ch, nil)` — streaming means the count is unknown upfront, same as the wiki adapter.

### Page field mapping

| `db.Page` field | Value |
|---|---|
| `SourceID` | Passed-in `sourceID`. |
| `URL` | `https://github.com/{repo}/blob/{branch}/{relativePath}`. Deep-link to the file on github.com. |
| `Title` | For `.md`/`.mdx`: first line matching `^# ` (after leading whitespace), with the `# ` stripped and trimmed. Fallback (always used for `.txt`, or if no H1 found): filename stem with `-` and `_` replaced by space. |
| `Content` | Raw file text (string conversion of the bytes). |
| `Path` | Folder segments after `include_path` is stripped, then the file stem. `docs/api/auth.md` with `include_path=docs/` → `["api", "auth"]`. A root-level file → `["auth"]`. Empty-segment collisions collapse. This shape lets `browse_source` reflect the repo's folder tree with no artificial root. |

## Error handling

| Failure | Behavior |
|---|---|
| `GET tarball` network/5xx error | `Crawl` returns the error before the goroutine starts (wrapped: `"github_repo: fetch tarball: %w"`). |
| HTTP 401 or 403 | Return wrapped error `"github_repo: unauthorized — token missing or lacks repo scope (status %d)"`. Crawler surfaces to the UI via the existing last-error mechanism. |
| HTTP 404 | Return wrapped error `"github_repo: repo or branch not found: %s@%s"`. Catches typos and the `main` vs `master` default-branch case. |
| HTTP 429 | Read `Retry-After` header, sleep up to 60 s, retry once, then error. Mirrors the web adapter. |
| gzip/tar decode error mid-stream | `slog.Error("github_repo: decode tarball", "err", err)`, close channel, end crawl. Pages already emitted before the failure remain in the DB. |
| Per-file size over 5 MiB cap | `slog.Warn` with path, skip, continue. |
| `ctx.Done()` mid-stream | Goroutine returns; channel closes via `defer`. |

All returned errors use `fmt.Errorf("context: %w", err)` wrapping per project convention.

## Testing

Test file `internal/adapter/githubrepo/githubrepo_test.go`. All tests run under `CGO_ENABLED=1 go test -tags sqlite_fts5 ./...` per project convention.

### Fixture helper

```go
func buildTarball(t *testing.T, entries map[string][]byte) []byte
```

Produces a gzipped tarball with `{prefix}/{path}` entries, using a fixed prefix like `owner-repo-abc123/`. Used to stand in for real GitHub tarball output in unit tests.

### Test cases

1. **Happy path, whole repo.** No `include_path`. Fixture has `README.md`, `docs/guide.md`, `docs/api/auth.md`, `image.png`, and `huge.md` (> 5 MiB). Assert three pages emitted, non-Markdown and oversized skipped.
2. **Subfolder filter.** `include_path=docs/` on the same fixture. Assert exactly two pages: `["guide"]` and `["api","auth"]`, each with `URL` deep-linking into `blob/main/docs/...`.
3. **Title extraction.** One file with `# Real Title\n\ncontent` → `Title == "Real Title"`. One file with no H1 → filename fallback (`getting-started.md` → `getting started`).
4. **Auth headers.** Test server asserts `Authorization: Bearer tok` is sent when `src.Token == "tok"`; no header when token is empty.
5. **Redirect auth drop.** Test server's tarball endpoint 302s to a second handler on the same `httptest.Server`; the second handler asserts `Authorization` is absent. (The real cross-host behavior is Go stdlib; this verifies our config doesn't defeat it.)
6. **Error mapping.** 404 response → wrapped error mentioning repo and branch. 401 → "unauthorized" error. 429 with `Retry-After: 0` then 200 → single retry then success and pages emitted.
7. **Context cancellation.** Start crawl, cancel context mid-stream, verify channel closes and goroutine exits without panic.
8. **Config validation.** `include_path="../secrets"` rejected at config load with a clear error.

### Integration touch-points (verified manually or in existing suites)

- `sourceToConfig` in `internal/crawler/crawler.go` — verify `Branch` (new) and `IncludePath` (existing) are forwarded from `db.Source` to `config.SourceConfig` for the `github_repo` type, with `Branch` defaulting to `"main"` when the stored value is empty. Per CLAUDE.md's `sourceToConfig` checklist.
- Web UI: add `github_repo` option to the source-type dropdown and a `branch` input; the existing `include_path` field is already reused.

## Out of scope / future work

- Incremental crawling via stored commit SHA and `If-None-Match` on the tarball URL.
- Auto-detection of the repo's default branch when `branch` is empty.
- Additional file formats (`.rst`, `.adoc`) via a configurable extension list.
- Additional git hosts (GitLab, Bitbucket, self-hosted, Azure Repos) as separate adapters.
