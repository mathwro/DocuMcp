# Design: Web Source Path Prefix Filter (`include_path`)

**Date:** 2026-03-05
**Status:** Approved

## Problem

When a documentation site has a root sitemap covering multiple sections (e.g. ArgoCD's `sitemap.xml` covering all of `/en/stable/`), users have no way to restrict indexing to a specific subsection (e.g. `/en/stable/operator-manual/`) without setting `url` to the subpath — which works but is not obvious and is undocumented in the UI.

## Solution

Add an optional `include_path` field to web sources. When set to a full URL prefix, only pages whose URL starts with that prefix are indexed. Behaviour is unchanged when the field is absent.

## Config Example

```yaml
sources:
  - name: ArgoCD Operator Manual
    type: web
    url: https://argo-cd.readthedocs.io/en/stable/
    include_path: https://argo-cd.readthedocs.io/en/stable/operator-manual/
    crawl_schedule: "@weekly"
```

## Changes

### `internal/config/config.go`
Add `IncludePath string \`yaml:"include_path,omitempty"\`` to `SourceConfig`.

### `internal/db/schema.go`
Add `include_path TEXT NOT NULL DEFAULT ''` column to `sources` table.

### `internal/db/db.go`
- Add `IncludePath string` to `Source` struct
- Add column to INSERT, both SELECT queries, and all Scan calls
- Add inline `ALTER TABLE sources ADD COLUMN include_path TEXT NOT NULL DEFAULT ''` migration (ignored if column already exists)

### `internal/adapter/web/web.go`
In `Crawl`, after URL discovery, if `src.IncludePath != ""`:
- Parse `include_path` as a URL
- Use its `.Path` as the prefix filter instead of `base.Path`
- Validate it shares the same origin as `src.URL` (return error if not)

### `internal/api/handlers.go`
Read `IncludePath` from the JSON request body when creating/updating a source (already automatic since `db.Source` is embedded in `sourceResponse`; the create handler maps fields explicitly so needs updating).

### `web/static/index.html` + `app.js`
- Add optional "Restrict to path prefix" `<input type="url">` field below the URL field, visible only for `web` type sources
- `newSource` object in Alpine data gets `IncludePath: ''`
- Field sent in POST body when adding a source

## Non-Changes
- Adapter interface: unchanged
- Crawler, MCP server, search: unchanged
- GitHub Wiki and Azure DevOps adapters: unchanged
- The filter logic path when `include_path` is empty: unchanged (existing `base.Path` prefix filter applies)
