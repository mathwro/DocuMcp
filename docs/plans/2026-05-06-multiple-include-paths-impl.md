# Multiple Include Paths Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `include_paths` so web and GitHub repo sources can be scoped to multiple path prefixes while preserving `include_path` compatibility.

**Architecture:** Add `IncludePaths []string` beside the existing `IncludePath string`, normalize both fields into one ordered set, and store the list in a new SQLite JSON-text column. Adapters consume normalized path lists and match pages/files against any configured prefix. The Web UI replaces the single ambiguous include-path input with a multi-entry "Crawl only these paths" control.

**Tech Stack:** Go 1.26, SQLite with JSON encoded text columns, YAML via `gopkg.in/yaml.v3`, Alpine.js static UI.

---

## File Structure

- `internal/config/config.go`: add `IncludePaths` to `SourceConfig`.
- `internal/config/config_test.go`: cover YAML loading for legacy and multi-path fields.
- `internal/sourcepaths/sourcepaths.go`: new small helper package for combining/deduping path lists.
- `internal/sourcepaths/sourcepaths_test.go`: unit tests for helper behavior.
- `internal/db/schema.go`: add `include_paths TEXT NOT NULL DEFAULT '[]'`.
- `internal/db/db.go`: add `Source.IncludePaths`, migration, JSON encode/decode helpers, and query updates.
- `internal/db/db_test.go`: cover insert/get/list/update compatibility.
- `internal/crawler/crawler.go` and `internal/crawler/crawler_internal_test.go`: forward `IncludePaths`.
- `internal/api/handlers.go` and `internal/api/handlers_test.go`: validate every path entry and return both fields.
- `internal/mcp/tools.go` and `internal/mcp/server_test.go` or a focused new internal MCP test: expose `includePaths`.
- `internal/adapter/web/web.go` and `internal/adapter/web/web_test.go`: parse/validate multiple URL prefixes and match any.
- `internal/adapter/githubrepo/githubrepo.go` and `internal/adapter/githubrepo/githubrepo_test.go`: normalize/validate multiple repo prefixes and match any.
- `web/static/app.js`: manage path arrays, request payloads, and display text.
- `web/static/index.html`: add multi-entry controls and clearer helper text.
- `web/static/style.css`: add modest styles if needed for row controls.
- `docs/configuration.md`, `docs/sources.md`, `config.example.yaml`: document `include_paths`.

## Task 1: Config Model And Normalization Helper

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/sourcepaths/sourcepaths.go`
- Create: `internal/sourcepaths/sourcepaths_test.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1.1: Write normalization helper tests**

Create `internal/sourcepaths/sourcepaths_test.go`:

```go
package sourcepaths

import (
	"reflect"
	"testing"
)

func TestNormalize_CombinesLegacyAndList(t *testing.T) {
	got := Normalize(" docs/ ", []string{"examples/", "", "docs/", " api/ "})
	want := []string{"docs/", "examples/", "api/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize() = %#v, want %#v", got, want)
	}
}

func TestNormalize_EmptyReturnsEmptySlice(t *testing.T) {
	got := Normalize("", nil)
	if got == nil {
		t.Fatal("Normalize returned nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("Normalize length = %d, want 0", len(got))
	}
}

func TestFirst_ReturnsFirstOrEmpty(t *testing.T) {
	if got := First([]string{"docs/", "api/"}); got != "docs/" {
		t.Fatalf("First() = %q, want docs/", got)
	}
	if got := First(nil); got != "" {
		t.Fatalf("First(nil) = %q, want empty", got)
	}
}
```

- [ ] **Step 1.2: Run helper tests and verify they fail**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/sourcepaths
```

Expected: fail because `internal/sourcepaths` does not exist.

- [ ] **Step 1.3: Implement the helper**

Create `internal/sourcepaths/sourcepaths.go`:

```go
package sourcepaths

import "strings"

// Normalize combines the legacy single path with the multi-path list.
// It trims blanks, drops empty entries, and deduplicates in first-seen order.
func Normalize(legacy string, paths []string) []string {
	out := make([]string, 0, len(paths)+1)
	seen := make(map[string]struct{}, len(paths)+1)
	for _, p := range append([]string{legacy}, paths...) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func First(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
```

- [ ] **Step 1.4: Run helper tests and verify they pass**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/sourcepaths
```

Expected: pass.

- [ ] **Step 1.5: Add `IncludePaths` to config**

In `internal/config/config.go`, update `SourceConfig`:

```go
URL          string   `yaml:"url,omitempty"`
IncludePath  string   `yaml:"include_path,omitempty"`
IncludePaths []string `yaml:"include_paths,omitempty"`
Auth         string   `yaml:"auth,omitempty"`
```

- [ ] **Step 1.6: Add YAML loading test**

Append to `internal/config/config_test.go`:

```go
func TestLoadConfig_IncludePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "include-paths.yaml")
	data := []byte(`sources:
  - name: Docs
    type: web
    url: https://docs.example.com/
    include_path: https://docs.example.com/legacy/
    include_paths:
      - https://docs.example.com/guides/
      - https://docs.example.com/reference/
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	src := cfg.Sources[0]
	if src.IncludePath != "https://docs.example.com/legacy/" {
		t.Fatalf("IncludePath = %q", src.IncludePath)
	}
	want := []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"}
	if !reflect.DeepEqual(src.IncludePaths, want) {
		t.Fatalf("IncludePaths = %#v, want %#v", src.IncludePaths, want)
	}
}
```

Add `reflect` to the imports.

- [ ] **Step 1.7: Run config tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/config
```

Expected: pass.

- [ ] **Step 1.8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/sourcepaths/sourcepaths.go internal/sourcepaths/sourcepaths_test.go
git commit -m "feat(config): add include_paths model"
```

## Task 2: Persist Multiple Include Paths

**Files:**
- Modify: `internal/db/schema.go`
- Modify: `internal/db/db.go`
- Modify: `internal/db/db_test.go`

- [ ] **Step 2.1: Add failing DB tests**

In `internal/db/db_test.go`, add:

```go
func TestInsertSource_PersistsIncludePaths(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{
		Name:         "Docs",
		Type:         "web",
		URL:          "https://docs.example.com",
		IncludePaths: []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"},
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	got, err := store.GetSource(id)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	want := []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"}
	if !reflect.DeepEqual(got.IncludePaths, want) {
		t.Fatalf("IncludePaths = %#v, want %#v", got.IncludePaths, want)
	}
	if got.IncludePath != want[0] {
		t.Fatalf("IncludePath = %q, want first path %q", got.IncludePath, want[0])
	}
}

func TestInsertSource_LegacyIncludePathPopulatesIncludePaths(t *testing.T) {
	store := testutil.OpenStore(t)

	id, err := store.InsertSource(db.Source{
		Name:        "Docs",
		Type:        "github_repo",
		Repo:        "owner/repo",
		IncludePath: "docs/",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	got, err := store.GetSource(id)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if !reflect.DeepEqual(got.IncludePaths, []string{"docs/"}) {
		t.Fatalf("IncludePaths = %#v, want [docs/]", got.IncludePaths)
	}
}
```

Add `reflect` to imports if absent.

- [ ] **Step 2.2: Run DB tests and verify they fail**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/db -run 'TestInsertSource_.*IncludePath'
```

Expected: fail because `db.Source.IncludePaths` does not exist.

- [ ] **Step 2.3: Add schema and migration**

In `internal/db/schema.go`, add `include_paths` after `include_path`:

```sql
    include_path   TEXT NOT NULL DEFAULT '',
    include_paths  TEXT NOT NULL DEFAULT '[]',
```

In `internal/db/db.go` `Open`, add:

```go
_, _ = db.Exec(`ALTER TABLE sources ADD COLUMN include_paths TEXT NOT NULL DEFAULT '[]'`)
```

- [ ] **Step 2.4: Add DB struct field and JSON helpers**

In `internal/db/db.go`, add to `Source`:

```go
IncludePath    string
IncludePaths   []string
```

Add helpers near `Source` or before `InsertSource`:

```go
func sourceIncludePaths(src Source) []string {
	return sourcepaths.Normalize(src.IncludePath, src.IncludePaths)
}

func encodeIncludePaths(paths []string) (string, error) {
	if paths == nil {
		paths = []string{}
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return "", fmt.Errorf("marshal include_paths: %w", err)
	}
	return string(data), nil
}

func decodeIncludePaths(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return nil, fmt.Errorf("unmarshal include_paths: %w", err)
	}
	if paths == nil {
		paths = []string{}
	}
	return paths, nil
}
```

Add import:

```go
"github.com/mathwro/DocuMcp/internal/sourcepaths"
```

- [ ] **Step 2.5: Update insert/select/update scans**

Update `InsertSource` to compute normalized paths:

```go
paths := sourceIncludePaths(src)
pathsJSON, err := encodeIncludePaths(paths)
if err != nil {
	return 0, err
}
legacyPath := sourcepaths.First(paths)
```

Then update the query:

```go
`INSERT INTO sources (name, type, url, repo, branch, base_url, space_key, auth, crawl_schedule, include_path, include_paths)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
src.Name, src.Type, src.URL, src.Repo, src.Branch, src.BaseURL, src.SpaceKey, src.Auth, src.CrawlSchedule, legacyPath, pathsJSON,
```

Update both `SELECT` lists to include `include_paths` after `include_path`. Scan into local `includePathsJSON string`, then call:

```go
paths, err := decodeIncludePaths(includePathsJSON)
if err != nil {
	return nil, fmt.Errorf("decode source include paths: %w", err)
}
src.IncludePaths = sourcepaths.Normalize(src.IncludePath, paths)
if src.IncludePath == "" {
	src.IncludePath = sourcepaths.First(src.IncludePaths)
}
```

Use the same logic in `ListSources` and `GetSource`, with error messages that include the operation.

Update `UpdateSourceConfig` with normalized paths and query:

```go
paths := sourceIncludePaths(src)
pathsJSON, err := encodeIncludePaths(paths)
if err != nil {
	return fmt.Errorf("update source config %d include paths: %w", id, err)
}
legacyPath := sourcepaths.First(paths)
```

```sql
SET name = ?, url = ?, repo = ?, branch = ?, base_url = ?, space_key = ?, crawl_schedule = ?, include_path = ?, include_paths = ?
```

- [ ] **Step 2.6: Run DB tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/db
```

Expected: pass.

- [ ] **Step 2.7: Commit**

```bash
git add internal/db/schema.go internal/db/db.go internal/db/db_test.go
git commit -m "feat(db): persist multiple include paths"
```

## Task 3: Forward And Expose Multiple Include Paths

**Files:**
- Modify: `internal/crawler/crawler.go`
- Modify: `internal/crawler/crawler_internal_test.go`
- Modify: `internal/mcp/tools.go`
- Test: `internal/mcp`

- [ ] **Step 3.1: Add crawler forwarding test**

In `internal/crawler/crawler_internal_test.go`, add:

```go
func TestSourceToConfig_ForwardsIncludePaths(t *testing.T) {
	got := sourceToConfig(db.Source{
		Type:         "web",
		URL:          "https://docs.example.com",
		IncludePath:  "https://docs.example.com/legacy/",
		IncludePaths: []string{"https://docs.example.com/guides/"},
	})
	if got.IncludePath != "https://docs.example.com/legacy/" {
		t.Fatalf("IncludePath = %q", got.IncludePath)
	}
	if !reflect.DeepEqual(got.IncludePaths, []string{"https://docs.example.com/guides/"}) {
		t.Fatalf("IncludePaths = %#v", got.IncludePaths)
	}
}
```

Add `reflect` to imports.

- [ ] **Step 3.2: Run crawler test and verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/crawler -run TestSourceToConfig_ForwardsIncludePaths
```

Expected: fail because `SourceConfig.IncludePaths` is not forwarded.

- [ ] **Step 3.3: Forward `IncludePaths`**

In `internal/crawler/crawler.go`, add:

```go
IncludePaths:  src.IncludePaths,
```

beside `IncludePath`.

- [ ] **Step 3.4: Expose MCP `includePaths`**

In `internal/mcp/tools.go`, add to `sourceInfo`:

```go
IncludePaths []string `json:"includePaths,omitempty"`
```

In `toSourceInfo`, add:

```go
IncludePaths: s.IncludePaths,
```

- [ ] **Step 3.5: Run crawler and MCP tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/crawler ./internal/mcp
```

Expected: pass.

- [ ] **Step 3.6: Commit**

```bash
git add internal/crawler/crawler.go internal/crawler/crawler_internal_test.go internal/mcp/tools.go
git commit -m "feat: expose multiple include paths"
```

## Task 4: API Validation And Round Trip

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/handlers_test.go`

- [ ] **Step 4.1: Add API update test for multiple paths**

In `internal/api/handlers_test.go`, add:

```go
func TestUpdateSource_IncludePaths(t *testing.T) {
	store := openTestStore(t)
	id, err := store.InsertSource(db.Source{
		Name: "Docs",
		Type: "web",
		URL:  "https://docs.example.com",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}

	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, err := json.Marshal(db.Source{
		Name:         "Docs",
		URL:          "https://docs.example.com",
		IncludePaths: []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/sources/"+itoa(id), bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated db.Source
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []string{"https://docs.example.com/guides/", "https://docs.example.com/reference/"}
	if !reflect.DeepEqual(updated.IncludePaths, want) {
		t.Fatalf("IncludePaths = %#v, want %#v", updated.IncludePaths, want)
	}
	if updated.IncludePath != want[0] {
		t.Fatalf("IncludePath = %q, want first path", updated.IncludePath)
	}
}

func TestCreateSource_IncludePathsRejectsBadURL(t *testing.T) {
	store := openTestStore(t)
	srv := api.NewServer(store, nil, nil, make([]byte, 32))
	body, _ := json.Marshal(db.Source{
		Name:         "Docs",
		Type:         "web",
		URL:          "https://docs.example.com",
		IncludePaths: []string{"https://docs.example.com/guides/", "://bad-url"},
	})

	r := httptest.NewRequest(http.MethodPost, "/api/sources", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
```

Add `reflect` to imports.

- [ ] **Step 4.2: Run API tests and verify failure if validation is missing**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/api -run 'Test(UpdateSource_IncludePaths|CreateSource_IncludePathsRejectsBadURL)'
```

Expected: one or both tests fail until DB and validation behavior are complete.

- [ ] **Step 4.3: Validate all include path entries**

In `internal/api/handlers.go`, import `sourcepaths` and update `validateSourceURLs`:

```go
for _, includePath := range sourcepaths.Normalize(src.IncludePath, src.IncludePaths) {
	if strings.Contains(includePath, "://") {
		if err := validateHTTPURL(includePath, "include_paths"); err != nil {
			return err
		}
	}
}
```

Remove or replace the old single `src.IncludePath` block. Keep the logic URL-shape based because `github_repo` paths are bare subpaths.

- [ ] **Step 4.4: Run API tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/api
```

Expected: pass.

- [ ] **Step 4.5: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat(api): accept multiple include paths"
```

## Task 5: Web Adapter Multiple Prefix Matching

**Files:**
- Modify: `internal/adapter/web/web.go`
- Modify: `internal/adapter/web/web_test.go`

- [ ] **Step 5.1: Add filter helper tests**

In `internal/adapter/web/web_test.go`, add:

```go
func TestFilterURL_IncludePathsFiltersCorrectly(t *testing.T) {
	withLookup(t, map[string][]net.IP{
		"docs.example.com": {net.ParseIP("1.2.3.4")},
	})
	base := mustParseURL("https://docs.example.com/docs/")
	filterPaths := []string{"/docs/guide/", "/docs/reference/"}

	cases := []struct {
		rawURL string
		want   bool
	}{
		{"https://docs.example.com/docs/guide/page1", true},
		{"https://docs.example.com/docs/reference/page2", true},
		{"https://docs.example.com/docs/api/page3", false},
	}

	for _, tc := range cases {
		u := mustParseURL(tc.rawURL)
		got := filterURLAny(context.Background(), u, base, filterPaths)
		if got != tc.want {
			t.Errorf("filterURLAny(%q) = %v, want %v", tc.rawURL, got, tc.want)
		}
	}
}
```

- [ ] **Step 5.2: Run web adapter test and verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/web -run TestFilterURL_IncludePathsFiltersCorrectly
```

Expected: fail because `filterURLAny` does not exist.

- [ ] **Step 5.3: Implement web include path parsing**

In `internal/adapter/web/web.go`, import `sourcepaths`.

Replace single `filterPath` setup with:

```go
filterPaths := []string{strings.TrimRight(base.Path, "/") + "/"}
includePaths := sourcepaths.Normalize(src.IncludePath, src.IncludePaths)
if len(includePaths) > 0 {
	filterPaths = make([]string, 0, len(includePaths))
	for _, includePath := range includePaths {
		includeParsed, parseErr := url.Parse(includePath)
		if parseErr != nil {
			return 0, nil, fmt.Errorf("web adapter: parse include_path: %w", parseErr)
		}
		if !sameOrigin(includeParsed, base) {
			return 0, nil, fmt.Errorf("web adapter: include_path %q must share origin with source URL %q", includePath, src.URL)
		}
		filterPaths = append(filterPaths, strings.TrimRight(includeParsed.Path, "/")+"/")
	}
}
```

In the sitemap loop, replace:

```go
if !filterURL(ctx, parsed, base, filterPath) {
```

with:

```go
if !filterURLAny(ctx, parsed, base, filterPaths) {
```

Add helper:

```go
func filterURLAny(ctx context.Context, u, base *url.URL, filterPaths []string) bool {
	for _, filterPath := range filterPaths {
		if filterURL(ctx, u, base, filterPath) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5.4: Run web adapter tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/web
```

Expected: pass.

- [ ] **Step 5.5: Commit**

```bash
git add internal/adapter/web/web.go internal/adapter/web/web_test.go
git commit -m "feat(web): crawl multiple include path prefixes"
```

## Task 6: GitHub Repo Adapter Multiple Prefix Matching

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo.go`
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 6.1: Add multi-subfolder test**

In `internal/adapter/githubrepo/githubrepo_test.go`, add:

```go
func TestCrawl_IncludePaths_ScopedToMultipleSubfolders(t *testing.T) {
	tarball := buildTarball(t, "owner-repo-abc123", map[string][]byte{
		"README.md":             []byte("# Root\n"),
		"docs/guide.md":         []byte("# Guide\n"),
		"examples/tutorial.md":  []byte("# Tutorial\n"),
		"internal/notes.md":     []byte("# Internal\n"),
	})
	srv := tarballServer(t, tarball)

	_, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
		Type:         "github_repo",
		Repo:         "owner/repo",
		Branch:       "main",
		IncludePaths: []string{"docs/", "examples/"},
	}, 42)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	pages := drainPages(context.Background(), t, ch)
	if len(pages) != 2 {
		t.Fatalf("got %d pages, want 2; URLs %v", len(pages), urls(pages))
	}
}

func TestCrawl_IncludePaths_RejectsTraversal(t *testing.T) {
	a := githubrepo.NewAdapter("https://api.github.test")
	_, _, err := a.Crawl(context.Background(), config.SourceConfig{
		Type:         "github_repo",
		Repo:         "owner/repo",
		IncludePaths: []string{"docs/", "../secrets"},
	}, 1)
	if err == nil {
		t.Fatal("expected error for traversal include_paths, got nil")
	}
	if !strings.Contains(err.Error(), "include_path") {
		t.Errorf("error should mention include_path: %v", err)
	}
}
```

- [ ] **Step 6.2: Run GitHub repo adapter tests and verify failure**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo -run 'TestCrawl_IncludePaths'
```

Expected: fail until adapter consumes `IncludePaths`.

- [ ] **Step 6.3: Implement multiple repo prefixes**

In `internal/adapter/githubrepo/githubrepo.go`, import `sourcepaths`.

At the top of `Crawl`, replace single `includePath` logic with:

```go
includePaths := normalizeIncludePaths(sourcepaths.Normalize(src.IncludePath, src.IncludePaths))
for _, includePath := range sourcepaths.Normalize(src.IncludePath, src.IncludePaths) {
	if err := validateIncludePath(includePath); err != nil {
		return 0, nil, err
	}
}
```

Add helpers:

```go
func normalizeIncludePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, normalizeIncludePath(p))
	}
	return out
}

func hasIncludePathPrefix(relPath string, includePaths []string) bool {
	if len(includePaths) == 0 {
		return true
	}
	for _, includePath := range includePaths {
		if strings.HasPrefix(relPath, includePath) {
			return true
		}
	}
	return false
}

func trimFirstIncludePathPrefix(relPath string, includePaths []string) string {
	for _, includePath := range includePaths {
		if strings.HasPrefix(relPath, includePath) {
			return strings.TrimPrefix(relPath, includePath)
		}
	}
	return relPath
}
```

Replace filtering:

```go
if includePath != "" && !strings.HasPrefix(relPath, includePath) {
	continue
}
```

with:

```go
if !hasIncludePathPrefix(relPath, includePaths) {
	continue
}
```

Change `buildPage` signature to:

```go
func buildPage(repo, branch string, includePaths []string, relPath, content string, sourceID int64) db.Page {
	rel := trimFirstIncludePathPrefix(relPath, includePaths)
```

Update call sites accordingly.

- [ ] **Step 6.4: Run GitHub repo adapter tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo
```

Expected: pass.

- [ ] **Step 6.5: Commit**

```bash
git add internal/adapter/githubrepo/githubrepo.go internal/adapter/githubrepo/githubrepo_test.go
git commit -m "feat(github_repo): index multiple include paths"
```

## Task 7: Web UI Multi-Path Control

**Files:**
- Modify: `web/static/app.js`
- Modify: `web/static/index.html`
- Modify: `web/static/style.css`

- [ ] **Step 7.1: Update Alpine source state helpers**

In `web/static/app.js`, update `blankSource`:

```js
function blankSource(type = 'web') {
  return { Name: '', Type: type, URL: '', Repo: '', Branch: '', IncludePath: '', IncludePaths: [], CrawlSchedule: '' }
}
```

Add helper functions before `app()`:

```js
function sourceIncludePaths(src) {
  const paths = Array.isArray(src.IncludePaths) ? src.IncludePaths : []
  const combined = [src.IncludePath || '', ...paths]
  return [...new Set(combined.map(p => (p || '').trim()).filter(Boolean))]
}

function prepareSourcePayload(src) {
  const paths = sourceIncludePaths(src)
  return { ...src, IncludePaths: paths, IncludePath: paths[0] || '' }
}
```

- [ ] **Step 7.2: Use normalized payloads**

In `addSource`, replace:

```js
const body = { ...this.newSource }
```

with:

```js
const body = prepareSourcePayload(this.newSource)
```

In `updateSource`, replace:

```js
const body = { ...this.editSource }
```

with:

```js
const body = prepareSourcePayload(this.editSource)
```

In `startEditSource`, set:

```js
IncludePath: src.IncludePath || '',
IncludePaths: sourceIncludePaths(src),
```

- [ ] **Step 7.3: Add Alpine methods for rows and labels**

Inside `app()` methods, add:

```js
includePathPlaceholder(type) {
  return type === 'github_repo' ? 'docs/' : 'https://docs.example.com/guides/'
},

includePathHelp(type) {
  if (type === 'github_repo') return 'Only files under these repository folders will be indexed. Leave empty to index the whole repo.'
  return 'Only same-site URLs under these prefixes will be indexed. Leave empty to use the source URL path.'
},

ensureIncludePathRow(src) {
  if (!Array.isArray(src.IncludePaths)) src.IncludePaths = []
  if (src.IncludePaths.length === 0) src.IncludePaths.push('')
},

addIncludePathRow(src) {
  if (!Array.isArray(src.IncludePaths)) src.IncludePaths = []
  src.IncludePaths.push('')
},

removeIncludePathRow(src, index) {
  if (!Array.isArray(src.IncludePaths)) src.IncludePaths = []
  src.IncludePaths.splice(index, 1)
},

sourceDisplayPaths(src) {
  return sourceIncludePaths(src).join(', ')
},
```

- [ ] **Step 7.4: Update source display URL for GitHub repo**

In `sourceDisplayURL`, keep showing the repo tree base without appending one include path:

```js
if (src.Type === 'github_repo' && src.Repo) {
  const branch = src.Branch || 'main'
  return `https://github.com/${src.Repo}/tree/${branch}`
}
```

- [ ] **Step 7.5: Replace add-form include path inputs**

In `web/static/index.html`, replace the separate single `IncludePath` templates for `web` and `github_repo` with a shared block shown for both:

```html
<template x-if="newSource.Type === 'web' || newSource.Type === 'github_repo'">
  <div class="path-scope" x-init="ensureIncludePathRow(newSource)">
    <label>Crawl only these paths</label>
    <p class="muted" x-text="includePathHelp(newSource.Type)"></p>
    <template x-for="(_, index) in newSource.IncludePaths" :key="index">
      <div class="path-row">
        <input :type="newSource.Type === 'web' ? 'url' : 'text'"
               x-model="newSource.IncludePaths[index]"
               :placeholder="includePathPlaceholder(newSource.Type)" />
        <button type="button" class="secondary" @click="removeIncludePathRow(newSource, index)">Remove</button>
      </div>
    </template>
    <button type="button" class="secondary" @click="addIncludePathRow(newSource)">Add path</button>
  </div>
</template>
```

- [ ] **Step 7.6: Replace edit-form include path inputs**

Use the same block with `editSource`:

```html
<template x-if="editSource.Type === 'web' || editSource.Type === 'github_repo'">
  <div class="path-scope">
    <label>Crawl only these paths</label>
    <p class="muted" x-text="includePathHelp(editSource.Type)"></p>
    <template x-for="(_, index) in editSource.IncludePaths" :key="index">
      <div class="path-row">
        <input :type="editSource.Type === 'web' ? 'url' : 'text'"
               x-model="editSource.IncludePaths[index]"
               :placeholder="includePathPlaceholder(editSource.Type)" />
        <button type="button" class="secondary" @click="removeIncludePathRow(editSource, index)">Remove</button>
      </div>
    </template>
    <button type="button" class="secondary" @click="addIncludePathRow(editSource)">Add path</button>
  </div>
</template>
```

- [ ] **Step 7.7: Display scoped paths in source metadata**

Under source metadata display, add:

```html
<div class="muted" x-show="sourceDisplayPaths(src)" x-text="'Crawls only: ' + sourceDisplayPaths(src)"></div>
```

- [ ] **Step 7.8: Add minimal styles**

In `web/static/style.css`, add:

```css
.path-scope {
  margin-bottom: 1rem;
}

.path-scope label {
  display: block;
  font-weight: 600;
  margin-bottom: 0.35rem;
}

.path-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.path-row input {
  flex: 1;
}
```

- [ ] **Step 7.9: Run static sanity checks**

Run:

```bash
rg -n "IncludePath|IncludePaths|Crawl only these paths|includePathHelp" web/static
```

Expected: no old single input remains for web/github_repo, and new helper text is present.

- [ ] **Step 7.10: Commit**

```bash
git add web/static/app.js web/static/index.html web/static/style.css
git commit -m "feat(ui): edit multiple include path filters"
```

## Task 8: Documentation And Examples

**Files:**
- Modify: `docs/configuration.md`
- Modify: `docs/sources.md`
- Modify: `config.example.yaml`

- [ ] **Step 8.1: Update config reference**

In `docs/configuration.md`, replace the `sources[].include_path` row with:

```markdown
| `sources[].include_path` | Legacy single path filter. Prefer `include_paths` for new config. |
| `sources[].include_paths` | For `web`: only same-origin URLs under these prefixes are indexed. For `github_repo`: only files under these repository folders are indexed. Empty means the current whole-source behavior. |
```

- [ ] **Step 8.2: Update source examples**

In `docs/sources.md`, update the web and GitHub repo examples to use `include_paths` with two entries. Add one sentence near each example:

```markdown
`include_paths` is a filter: only matching pages are indexed. It does not add extra crawl roots.
```

For GitHub repo:

```markdown
`include_paths` is a filter: only matching files are indexed. It does not add extra repositories or branches.
```

- [ ] **Step 8.3: Update example config**

In `config.example.yaml`, update commented examples:

```yaml
  #   include_paths:
  #     - https://argo-cd.readthedocs.io/en/stable/operator-manual/
  #     - https://argo-cd.readthedocs.io/en/stable/user-guide/
```

and:

```yaml
  #   include_paths:
  #     - docs/
  #     - examples/
```

- [ ] **Step 8.4: Run docs grep**

Run:

```bash
rg -n "include_path|include_paths|Crawl only" docs config.example.yaml
```

Expected: docs mention `include_paths` as preferred and `include_path` as legacy compatibility.

- [ ] **Step 8.5: Commit**

```bash
git add docs/configuration.md docs/sources.md config.example.yaml
git commit -m "docs: document multiple include path filters"
```

## Task 9: Final Verification

**Files:**
- All modified files

- [ ] **Step 9.1: Format Go files**

Run:

```bash
gofmt -w internal/config internal/sourcepaths internal/db internal/crawler internal/api internal/mcp internal/adapter/web internal/adapter/githubrepo
```

Expected: no output.

- [ ] **Step 9.2: Run vet**

Run:

```bash
CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...
```

Expected: pass.

- [ ] **Step 9.3: Run race tests**

Run:

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 -race ./...
```

Expected: pass.

- [ ] **Step 9.4: Optional local UI check**

Run:

```bash
make build
```

Then start the app using the repo's normal local command and verify:

- Add Source for `web` shows **Crawl only these paths** and same-site helper text.
- Add Source for `github_repo` shows repository-folder helper text.
- Adding/removing rows works.
- Edit Source initializes existing `IncludePaths`, or falls back from legacy `IncludePath`.
- The submitted payload contains `IncludePaths` and first-path `IncludePath`.

- [ ] **Step 9.5: Final commit if formatting changed files**

If Step 9.1 changed any files after earlier commits:

```bash
git add .
git commit -m "chore: format multiple include paths changes"
```

## Self-Review Checklist

- Design coverage: config, DB, crawler, API, MCP, web adapter, GitHub repo adapter, UI, docs, and tests are all represented.
- Compatibility: `include_path` remains accepted, stored, and returned.
- UI wording: the plan uses "Crawl only these paths" and explicit filter helper text.
- Scope: GitHub Wiki and Azure DevOps are intentionally unchanged.
- Verification: final `gofmt`, `go vet`, and race tests are included with the required `CGO_ENABLED=1` and `sqlite_fts5` tag.
