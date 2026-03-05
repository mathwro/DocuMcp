# include_path Feature Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an optional `include_path` field to web sources that restricts crawling to a specific URL prefix, with UI support for setting it.

**Architecture:** The field flows from config YAML → DB column → web adapter URL filter. When `include_path` is set, its path replaces the source URL's path as the crawl prefix filter. The API handler already decodes `db.Source` directly from JSON so no handler changes are needed. The UI adds an optional field visible only for web sources.

**Tech Stack:** Go (config, db, adapter), Alpine.js (UI), SQLite migration pattern already established in this codebase.

**Build/test commands (always required):**
```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags sqlite_fts5 ./...
```

---

### Task 1: Add `IncludePath` to config and DB

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/db/schema.go`
- Modify: `internal/db/db.go`

**Step 1: Add field to `SourceConfig` in `internal/config/config.go`**

After the `URL` field, add:
```go
IncludePath string `yaml:"include_path,omitempty"`
```

**Step 2: Add column to schema in `internal/db/schema.go`**

After the `page_count` and `crawl_total` lines:
```sql
include_path   TEXT NOT NULL DEFAULT '',
```

**Step 3: Add migration + struct field + query updates in `internal/db/db.go`**

Add to the `Source` struct after `CrawlTotal`:
```go
IncludePath string
```

Add inline migration after the existing `crawl_total` migration line:
```go
_, _ = db.Exec(`ALTER TABLE sources ADD COLUMN include_path TEXT NOT NULL DEFAULT ''`)
```

Update `InsertSource` query to include `include_path`:
```go
`INSERT INTO sources (name, type, url, repo, base_url, space_key, auth, crawl_schedule, include_path)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
src.Name, src.Type, src.URL, src.Repo, src.BaseURL, src.SpaceKey, src.Auth, src.CrawlSchedule, src.IncludePath,
```

Update both SELECT queries (`ListSources` and `GetSource`) to include `include_path` in the column list and add `&src.IncludePath` at the end of each Scan call.

**Step 4: Build to verify no compilation errors**

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
```
Expected: no output (clean build).

**Step 5: Run tests**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/db/... ./internal/config/...
```
Expected: all pass.

**Step 6: Commit**

```bash
git add internal/config/config.go internal/db/schema.go internal/db/db.go
git commit -m "feat: add include_path field to config, db schema, and Source struct"
```

---

### Task 2: Use `include_path` in the web adapter

**Files:**
- Modify: `internal/adapter/web/web.go`
- Test: `internal/adapter/web/web_test.go` (or wherever web crawl tests live — check with `ls internal/adapter/web/`)

**Step 1: Write a failing test**

In `internal/adapter/web/web_test.go` (or a new file if none exists for Crawl), add a test that creates a web adapter with a mock sitemap server and verifies that when `include_path` is set, URLs outside that prefix are excluded.

Check existing test files first:
```bash
ls internal/adapter/web/
cat internal/adapter/web/extract_test.go   # to see test patterns
```

Add to the web adapter test file:
```go
func TestCrawl_IncludePath_FiltersURLs(t *testing.T) {
    // Serve a sitemap with two paths: /docs/api/ and /docs/guide/
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/sitemap.xml":
            fmt.Fprint(w, `<?xml version="1.0"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>`+srv.URL+`/docs/api/page1</loc></url>
  <url><loc>`+srv.URL+`/docs/guide/page2</loc></url>
</urlset>`)
        default:
            fmt.Fprintf(w, "<html><head><title>Page</title></head><body><h1>%s</h1></body></html>", r.URL.Path)
        }
    }))
    defer srv.Close()

    a := &WebAdapter{}
    total, ch, err := a.Crawl(context.Background(), config.SourceConfig{
        Type:        "web",
        URL:         srv.URL + "/docs/",
        IncludePath: srv.URL + "/docs/guide/",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl error: %v", err)
    }
    if total != 1 {
        t.Errorf("expected total=1, got %d", total)
    }
    var pages []db.Page
    for p := range ch {
        pages = append(pages, p)
    }
    if len(pages) != 1 {
        t.Errorf("expected 1 page, got %d", len(pages))
    }
    if len(pages) > 0 && !strings.Contains(pages[0].URL, "/docs/guide/") {
        t.Errorf("expected guide page, got %s", pages[0].URL)
    }
}
```

Note: the test server's loopback address (`127.0.0.1`) is blocked by `isAllowedHost`. To work around this, run the test with the `isAllowedHost` function bypassed, or use the same stub-adapter pattern from `crawler_test.go`. If the test can't use a real HTTP server, test the URL filtering logic directly by extracting it into a helper function (see Step 3).

**Step 2: Run the test to see it fail**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/web/... -run TestCrawl_IncludePath -v
```
Expected: FAIL — either compilation error or test failure.

**Step 3: Implement `include_path` filtering in `internal/adapter/web/web.go`**

In the `Crawl` function, after parsing `base`, add logic to determine the effective filter path:

```go
// Determine the path prefix to use for URL filtering.
// If include_path is set, validate it shares the same origin and use its path.
// Otherwise fall back to the source URL's own path.
filterPath := strings.TrimRight(base.Path, "/") + "/"
if src.IncludePath != "" {
    includeParsed, err := url.Parse(src.IncludePath)
    if err != nil {
        return 0, nil, fmt.Errorf("web adapter: parse include_path: %w", err)
    }
    if !sameOrigin(includeParsed, base) {
        return 0, nil, fmt.Errorf("web adapter: include_path %q must share origin with source URL %q", src.IncludePath, src.URL)
    }
    filterPath = strings.TrimRight(includeParsed.Path, "/") + "/"
}
```

Then replace the existing `basePath` variable and its uses with `filterPath`:
- Remove: `basePath := strings.TrimRight(base.Path, "/") + "/"`
- Replace `basePath` with `filterPath` in the HasPrefix check

**Step 4: Run the test to see it pass**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/web/... -v
```
Expected: all pass.

**Step 5: Commit**

```bash
git add internal/adapter/web/web.go internal/adapter/web/
git commit -m "feat: apply include_path as URL prefix filter in web adapter"
```

---

### Task 3: Add `include_path` to the Web UI

**Files:**
- Modify: `web/static/app.js`
- Modify: `web/static/index.html`

**Step 1: Add `IncludePath` to the `newSource` default in `app.js`**

Change:
```js
newSource: { Name: '', Type: 'web', URL: '', Repo: '', CrawlSchedule: '' },
```
To:
```js
newSource: { Name: '', Type: 'web', URL: '', Repo: '', IncludePath: '', CrawlSchedule: '' },
```

Also update both reset lines (after successful add) to include `IncludePath: ''`.

**Step 2: Add the input field to `index.html`**

In the Add Source form, after the web URL input (`<input type="url" x-model="newSource.URL" ...>`), add:

```html
<template x-if="newSource.Type === 'web'">
  <input type="url" x-model="newSource.IncludePath"
         placeholder="Restrict to path prefix (optional, e.g. https://docs.example.com/guide/)" />
</template>
```

**Step 3: Build the container and test manually**

```bash
podman build -t documcp:local .
podman compose down && podman compose up -d
```

Open `http://localhost:8080`, go to Sources → Add Source, select type "web", verify the new field appears below the URL field with the placeholder text.

**Step 4: Commit**

```bash
git add web/static/app.js web/static/index.html
git commit -m "feat: add include_path field to Add Source form in web UI"
```

---

### Task 4: Update README

**Files:**
- Modify: `README.md`

**Step 1: Add `include_path` to the Web source config example and table**

In the **Web** source type section, update the example:
```yaml
- name: ArgoCD Operator Manual
  type: web
  url: https://argo-cd.readthedocs.io/en/stable/
  include_path: https://argo-cd.readthedocs.io/en/stable/operator-manual/
  crawl_schedule: "@weekly"
```

Add a row to the configuration table:

| `sources[].include_path` | (Web only) If set, only pages whose URL starts with this prefix are indexed. Must share the same origin as `url`. |

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document include_path option in README"
```
