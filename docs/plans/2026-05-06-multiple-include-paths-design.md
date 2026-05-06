# Design: Multiple Source Path Prefix Filters (`include_paths`)

**Date:** 2026-05-06
**Status:** Approved

## Problem

Sources can currently be scoped with one `include_path`. For web sources this limits crawling to one same-origin URL prefix. For `github_repo` sources this limits indexing to one repository subfolder. Some documentation sets are split across multiple relevant sections, so one source either has to crawl too broadly or be duplicated into several sources.

The existing name also reads like an additive option. In practice it is a scope filter: only matching pages or files are indexed.

## Solution

Add `include_paths` as the canonical multi-path field for `web` and `github_repo` sources. Keep the existing `include_path` field as a backwards-compatible single-path alias for existing YAML files, database rows, API clients, and UI-created records.

```yaml
sources:
  - name: Product Docs
    type: web
    url: https://docs.example.com/
    include_paths:
      - https://docs.example.com/guides/
      - https://docs.example.com/reference/
    crawl_schedule: "@weekly"

  - name: Repo Docs
    type: github_repo
    repo: owner/repo
    branch: main
    include_paths:
      - docs/
      - examples/
    crawl_schedule: "@daily"
```

When `include_paths` contains multiple entries, a page or file is indexed if it matches any configured prefix. Empty `include_paths` preserves current behavior.

## Field Semantics

### Canonical Field

`include_paths` is the new primary field:

```go
IncludePaths []string `yaml:"include_paths,omitempty"`
```

`include_path` remains available:

```go
IncludePath string `yaml:"include_path,omitempty"`
```

The system accepts three shapes:

- `include_path` only: existing single-path behavior.
- `include_paths` only: new multi-path behavior.
- Both fields: union the values, deduplicate after normalization, and preserve first-seen order.

DB/API structs should also gain `IncludePaths []string` while retaining `IncludePath string`.

### Normalization

Add a focused helper that returns normalized include paths for adapters and persistence. It should:

- Combine `IncludePath` and `IncludePaths`.
- Trim whitespace.
- Drop empty entries.
- Deduplicate while preserving order.
- Return an empty slice, not `nil`.

Normalization should not silently convert invalid values into valid ones. Adapter-specific validation still owns source-type rules.

## Web Source Behavior

For `type: web`:

- Each include path must parse as an HTTP(S) URL with a host.
- Each include path must share origin with the source `url`.
- The crawler keeps sitemap URLs whose path matches any include prefix.
- If no include paths are configured, the current fallback remains: filter by the source URL path.

Example:

```yaml
- name: Framework Docs
  type: web
  url: https://docs.example.com/
  include_paths:
    - https://docs.example.com/tutorials/
    - https://docs.example.com/api/
```

This indexes URLs under `/tutorials/` or `/api/`, not the rest of the site.

## GitHub Repo Source Behavior

For `type: github_repo`:

- Each include path is a repository-relative folder prefix.
- Leading slashes may be normalized away as they are today.
- Non-empty prefixes should compare with a trailing slash to avoid accidental partial matches.
- Any entry containing `..` traversal is rejected before fetching the tarball.
- Files are indexed when their repo-relative path matches any normalized prefix.
- If no include paths are configured, the whole repository is eligible.

Example:

```yaml
- name: Repo Docs
  type: github_repo
  repo: owner/repo
  branch: main
  include_paths:
    - docs/
    - examples/tutorials/
```

This indexes Markdown, MDX, and text files under those folders only.

## Persistence

Add a new SQLite column:

```sql
include_paths TEXT NOT NULL DEFAULT '[]'
```

Store `include_paths` as JSON text. Keep the existing `include_path` column.

On read:

- Decode `include_paths`.
- Combine it with `include_path` through the normalization helper.
- Populate `Source.IncludePaths` with the full normalized list.
- Keep `Source.IncludePath` populated with the legacy single value. If the legacy column is empty and the normalized list has entries, use the first entry.

On insert/update:

- Store the normalized list in `include_paths`.
- Store the first normalized entry in `include_path`, or `""` if the list is empty.

This keeps old clients working while making the multi-path field authoritative for new code.

## API And MCP Shape

JSON source payloads should accept and return both fields:

```json
{
  "Name": "Product Docs",
  "Type": "web",
  "URL": "https://docs.example.com/",
  "IncludePaths": [
    "https://docs.example.com/guides/",
    "https://docs.example.com/reference/"
  ],
  "IncludePath": "https://docs.example.com/guides/"
}
```

Validation should check every entry in `IncludePaths`, plus the legacy `IncludePath` when supplied.

MCP source listings should add `includePaths` while keeping `includePath` for compatibility.

## Web UI

The Web UI must be updated as part of this feature.

Replace the single include-path input in add/edit forms for `web` and `github_repo` sources with a compact multi-entry control:

- Group label: **Crawl only these paths**
- Button: **Add path**
- Web placeholder: `https://docs.example.com/guides/`
- GitHub repo placeholder: `docs/`

Helper text for web sources:

> Only same-site URLs under these prefixes will be indexed. Leave empty to use the source URL path.

Helper text for GitHub repo sources:

> Only files under these repository folders will be indexed. Leave empty to index the whole repo.

The UI should:

- Initialize add forms with `IncludePaths: []`.
- Initialize edit forms from `IncludePaths`, falling back to `[IncludePath]` for old records.
- Send `IncludePaths` in create/update payloads.
- Also send `IncludePath` as the first path for legacy server compatibility during the transition.
- Display multiple scoped paths clearly in source metadata, without implying they are additional crawl roots.

## Implementation Notes

Likely files to touch:

- `internal/config/config.go`: add `IncludePaths` to `SourceConfig`.
- `internal/db/schema.go`: add `include_paths` to the base schema.
- `internal/db/db.go`: add migration, JSON encode/decode, insert/select/update support, and `Source.IncludePaths`.
- `internal/crawler/crawler.go`: forward `IncludePaths` in `sourceToConfig`.
- `internal/api/handlers.go`: validate every configured web URL path entry.
- `internal/mcp/tools.go`: expose `includePaths`.
- `internal/adapter/web/web.go`: evaluate multiple URL path prefixes.
- `internal/adapter/githubrepo/githubrepo.go`: evaluate multiple repo folder prefixes and validate traversal per entry.
- `web/static/app.js`: manage path arrays in add/edit state, payloads, and source display.
- `web/static/index.html`: replace the single input with multi-entry controls and clearer helper text.
- `docs/configuration.md`, `docs/sources.md`, and `config.example.yaml`: document `include_paths` and clarify that these paths are filters.

## Testing

Add or update tests for:

- YAML loading with legacy `include_path`, new `include_paths`, and both fields together.
- DB insert/list/get/update preserving multiple paths and populating legacy `IncludePath`.
- API create/update validation across multiple web include paths.
- Web adapter matching any of several same-origin URL prefixes.
- Web adapter rejecting any cross-origin or malformed include path.
- GitHub repo adapter matching any of several subfolder prefixes.
- GitHub repo adapter rejecting `..` traversal in any include path.
- `sourceToConfig` forwarding both legacy and multi-path fields.
- MCP source conversion returning both `includePath` and `includePaths`.

Before PR, run:

```bash
gofmt -w .
CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags sqlite_fts5 -race ./...
```

For the UI, run the app locally and manually verify add/edit forms for both `web` and `github_repo` sources show the new wording, preserve multiple rows, and submit the expected payload.

## Non-Goals

- Do not remove `include_path`.
- Do not change GitHub Wiki or Azure DevOps source scoping.
- Do not introduce exclusion paths.
- Do not make web include paths cross-origin crawl roots.
- Do not write UI-managed sources back to YAML.
