# GitHub Repo Source Adapter — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `github_repo` source adapter that indexes `.md`/`.mdx`/`.txt` files from a GitHub repository, optionally scoped to a subfolder, by streaming a single repo tarball through `archive/tar`.

**Architecture:** New adapter package `internal/adapter/githubrepo` registered at init-time. Persists a new `branch` column on `sources` (default `"main"`). Reuses the existing GitHub device-code auth flow (`providerForType` extended, `authStart` condition extended). Streams tarball over `gzip.NewReader` + `tar.NewReader` — no temp files. Web UI gains a source-type option, a branch input, and the existing `include_path` input.

**Tech Stack:** Go 1.26, stdlib only for the adapter (`net/http`, `archive/tar`, `compress/gzip`, `io`, `strings`, `path`, `log/slog`). Tests use `net/http/httptest`. SQLite migrations via idempotent `ALTER TABLE` in `Open()`. Build flags: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./...`.

**Spec:** `docs/plans/2026-04-16-github-repo-source-design.md`.

---

## File Structure

**Created:**
- `internal/adapter/githubrepo/githubrepo.go` — adapter struct, `init()` registration, `NeedsAuth`, `Crawl`, title extraction helper, tarball streaming.
- `internal/adapter/githubrepo/githubrepo_test.go` — all adapter tests + tarball fixture helper.

**Modified:**
- `internal/config/config.go` — add `Branch` field to `SourceConfig`.
- `internal/db/db.go` — add `Branch` to `Source` struct; add migration; update `InsertSource`, `ListSources`, `GetSource`.
- `internal/db/schema.go` — add `branch` column to `sources` table.
- `internal/db/db_test.go` — round-trip test for `branch`.
- `internal/crawler/crawler.go` — extend `providerForType`; extend `sourceToConfig` with Branch + `"main"` default; add blank import for the new adapter.
- `internal/crawler/crawler_test.go` (or equivalent) — test `providerForType`/`sourceToConfig` for `github_repo`.
- `internal/api/handlers.go` — extend `authStart` GitHub-flow branch to include `github_repo`.
- `internal/api/handlers_test.go` — assertion that `authStart` for a `github_repo` source starts a GitHub device flow.
- `web/static/index.html` — add `github_repo` option, `branch` input, reuse `include_path` input.
- `web/static/app.js` — add `github_repo` to `sourceTypeName` label map.

---

## Task 1: Plumb `Branch` field end-to-end (config, db, schema, crawler)

**Files:**
- Modify: `internal/config/config.go:20-36`
- Modify: `internal/db/db.go:29-43, 68-71, 80-91, 93-116, 118-134`
- Modify: `internal/db/schema.go:3-19`
- Modify: `internal/crawler/crawler.go:41-50, 126-139`
- Test: `internal/db/db_test.go`, `internal/crawler/crawler_test.go`

- [ ] **Step 1.1: Write the failing DB round-trip test**

Add to `internal/db/db_test.go`:

```go
func TestInsertSource_github_repo_persists_branch(t *testing.T) {
    store := openTestStore(t)
    defer store.Close()

    id, err := store.InsertSource(Source{
        Name:        "example",
        Type:        "github_repo",
        Repo:        "owner/example",
        Branch:      "develop",
        IncludePath: "docs/",
    })
    if err != nil {
        t.Fatalf("InsertSource: %v", err)
    }
    got, err := store.GetSource(id)
    if err != nil {
        t.Fatalf("GetSource: %v", err)
    }
    if got.Branch != "develop" {
        t.Errorf("Branch: got %q, want %q", got.Branch, "develop")
    }
    if got.IncludePath != "docs/" {
        t.Errorf("IncludePath: got %q, want %q", got.IncludePath, "docs/")
    }
}
```

If `db_test.go` does not yet have `openTestStore`, inspect the file and reuse its existing test helper (or copy the pattern from another `db_test.go` test).

- [ ] **Step 1.2: Run the test, confirm it fails to compile**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/db/ -run TestInsertSource_github_repo_persists_branch
```

Expected: compile error `unknown field Branch in struct literal of type db.Source`.

- [ ] **Step 1.3: Add `Branch` to `config.SourceConfig`**

In `internal/config/config.go`, add the field next to the other github_wiki-related fields:

```go
type SourceConfig struct {
    Name string `yaml:"name"`
    Type string `yaml:"type"`
    // github_wiki, github_repo
    Repo   string `yaml:"repo,omitempty"`
    Branch string `yaml:"branch,omitempty"`
    // web
    URL         string `yaml:"url,omitempty"`
    IncludePath string `yaml:"include_path,omitempty"`
    Auth string `yaml:"auth,omitempty"`
    // confluence
    BaseURL  string `yaml:"base_url,omitempty"`
    SpaceKey string `yaml:"space_key,omitempty"`
    // scheduling
    CrawlSchedule string `yaml:"crawl_schedule,omitempty"`
    // Token is populated at runtime from the token store and never read from YAML.
    Token string `yaml:"-"`
}
```

- [ ] **Step 1.4: Add `Branch` to `db.Source`**

In `internal/db/db.go`:

```go
type Source struct {
    ID            int64
    Name          string
    Type          string
    URL           string
    Repo          string
    Branch        string
    BaseURL       string
    SpaceKey      string
    Auth          string
    CrawlSchedule string
    LastCrawled   *time.Time
    PageCount     int
    CrawlTotal    int
    IncludePath   string
}
```

- [ ] **Step 1.5: Add `branch` to schema**

In `internal/db/schema.go`, add the column to the `sources` table create statement (right after `repo`):

```sql
    repo           TEXT NOT NULL DEFAULT '',
    branch         TEXT NOT NULL DEFAULT '',
```

- [ ] **Step 1.6: Add idempotent migration**

In `internal/db/db.go` `Open()` after the existing migrations:

```go
_, _ = db.Exec(`ALTER TABLE sources ADD COLUMN crawl_total INTEGER NOT NULL DEFAULT 0`)
_, _ = db.Exec(`ALTER TABLE sources ADD COLUMN include_path TEXT NOT NULL DEFAULT ''`)
_, _ = db.Exec(`ALTER TABLE sources ADD COLUMN branch TEXT NOT NULL DEFAULT ''`)
```

- [ ] **Step 1.7: Update `InsertSource`**

In `internal/db/db.go`:

```go
func (s *Store) InsertSource(src Source) (int64, error) {
    res, err := s.db.Exec(
        `INSERT INTO sources (name, type, url, repo, branch, base_url, space_key, auth, crawl_schedule, include_path)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        src.Name, src.Type, src.URL, src.Repo, src.Branch, src.BaseURL, src.SpaceKey, src.Auth, src.CrawlSchedule, src.IncludePath,
    )
    if err != nil {
        return 0, fmt.Errorf("insert source: %w", err)
    }
    return res.LastInsertId()
}
```

- [ ] **Step 1.8: Update `ListSources`**

```go
func (s *Store) ListSources() ([]Source, error) {
    rows, err := s.db.Query(
        `SELECT id, name, type, url, repo, branch, base_url, space_key, auth, crawl_schedule, page_count, last_crawled, crawl_total, include_path
         FROM sources ORDER BY id`,
    )
    if err != nil {
        return nil, fmt.Errorf("list sources: %w", err)
    }
    defer rows.Close()

    sources := make([]Source, 0)
    for rows.Next() {
        var src Source
        if err := rows.Scan(
            &src.ID, &src.Name, &src.Type, &src.URL, &src.Repo, &src.Branch,
            &src.BaseURL, &src.SpaceKey, &src.Auth, &src.CrawlSchedule, &src.PageCount, &src.LastCrawled, &src.CrawlTotal, &src.IncludePath,
        ); err != nil {
            return nil, fmt.Errorf("scan source: %w", err)
        }
        sources = append(sources, src)
    }
    return sources, rows.Err()
}
```

- [ ] **Step 1.9: Update `GetSource`**

```go
func (s *Store) GetSource(id int64) (*Source, error) {
    var src Source
    err := s.db.QueryRow(
        `SELECT id, name, type, url, repo, branch, base_url, space_key, auth, crawl_schedule, page_count, last_crawled, crawl_total, include_path
         FROM sources WHERE id = ?`, id,
    ).Scan(
        &src.ID, &src.Name, &src.Type, &src.URL, &src.Repo, &src.Branch,
        &src.BaseURL, &src.SpaceKey, &src.Auth, &src.CrawlSchedule, &src.PageCount, &src.LastCrawled, &src.CrawlTotal, &src.IncludePath,
    )
    if errors.Is(err, sql.ErrNoRows) {
        return nil, fmt.Errorf("source %d: %w", id, ErrNotFound)
    }
    if err != nil {
        return nil, fmt.Errorf("get source %d: %w", id, err)
    }
    return &src, nil
}
```

- [ ] **Step 1.10: Extend `providerForType` for `github_repo`**

In `internal/crawler/crawler.go`:

```go
func providerForType(sourceType string) string {
    switch sourceType {
    case "github_wiki", "github_repo":
        return "github"
    case "azure_devops":
        return "microsoft"
    default:
        return ""
    }
}
```

- [ ] **Step 1.11: Update `sourceToConfig` to forward Branch with `"main"` default**

In `internal/crawler/crawler.go`:

```go
func sourceToConfig(src db.Source) config.SourceConfig {
    branch := src.Branch
    if branch == "" {
        branch = "main"
    }
    return config.SourceConfig{
        Name:          src.Name,
        Type:          src.Type,
        Repo:          src.Repo,
        Branch:        branch,
        URL:           src.URL,
        Auth:          src.Auth,
        BaseURL:       src.BaseURL,
        SpaceKey:      src.SpaceKey,
        CrawlSchedule: src.CrawlSchedule,
        IncludePath:   src.IncludePath,
    }
}
```

- [ ] **Step 1.12: Add a crawler-layer test for the `"main"` default**

Create or extend `internal/crawler/crawler_test.go`:

```go
func TestSourceToConfig_github_repo_defaults_branch_to_main(t *testing.T) {
    got := sourceToConfig(db.Source{Type: "github_repo", Repo: "o/r", Branch: ""})
    if got.Branch != "main" {
        t.Errorf("Branch default: got %q, want %q", got.Branch, "main")
    }

    explicit := sourceToConfig(db.Source{Type: "github_repo", Repo: "o/r", Branch: "develop"})
    if explicit.Branch != "develop" {
        t.Errorf("Branch override: got %q, want %q", explicit.Branch, "develop")
    }
}

func TestProviderForType_github_repo(t *testing.T) {
    if providerForType("github_repo") != "github" {
        t.Errorf("expected provider 'github' for github_repo")
    }
}
```

If `crawler_test.go` does not already have `package crawler` (internal test package), put these as internal tests so they can call unexported `sourceToConfig`/`providerForType`.

- [ ] **Step 1.13: Run all affected tests, confirm they pass**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/db/ ./internal/crawler/ -v
```

Expected: `TestInsertSource_github_repo_persists_branch`, `TestSourceToConfig_github_repo_defaults_branch_to_main`, `TestProviderForType_github_repo` all PASS. All pre-existing tests still pass.

- [ ] **Step 1.14: Commit**

```bash
git add internal/config/config.go internal/db/ internal/crawler/
git commit -m "feat(db): add branch field for github_repo source type"
```

---

## Task 2: Register empty `githubrepo` adapter

**Files:**
- Create: `internal/adapter/githubrepo/githubrepo.go`
- Create: `internal/adapter/githubrepo/githubrepo_test.go`
- Modify: `internal/crawler/crawler.go:1-15` (blank-import block)

- [ ] **Step 2.1: Write the failing registration test**

Create `internal/adapter/githubrepo/githubrepo_test.go`:

```go
package githubrepo_test

import (
    "testing"

    "github.com/documcp/documcp/internal/adapter"
    _ "github.com/documcp/documcp/internal/adapter/githubrepo"
)

func TestAdapterRegistered(t *testing.T) {
    a, ok := adapter.Registry["github_repo"]
    if !ok {
        t.Fatal("github_repo adapter not registered")
    }
    if a == nil {
        t.Fatal("github_repo adapter is nil")
    }
}
```

- [ ] **Step 2.2: Run test, expect build failure**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/
```

Expected: `no Go files in internal/adapter/githubrepo` or `cannot find package`.

- [ ] **Step 2.3: Create adapter skeleton**

Create `internal/adapter/githubrepo/githubrepo.go`:

```go
// Package githubrepo indexes .md, .mdx, and .txt files from a GitHub
// repository (optionally scoped to a subfolder) by streaming a single
// tarball download through archive/tar.
package githubrepo

import (
    "context"

    "github.com/documcp/documcp/internal/adapter"
    "github.com/documcp/documcp/internal/config"
    "github.com/documcp/documcp/internal/db"
)

func init() {
    adapter.Register("github_repo", NewAdapter("https://api.github.com"))
}

// Adapter streams a GitHub repo tarball and emits db.Page entries for
// matching documentation files.
type Adapter struct{ baseURL string }

// NewAdapter constructs an Adapter with the given GitHub API base URL.
// The baseURL parameter enables test injection of a local httptest server.
func NewAdapter(baseURL string) *Adapter {
    return &Adapter{baseURL: baseURL}
}

// NeedsAuth always returns true: private repos require a token, and
// returning true surfaces the auth-setup UI for all github_repo sources.
func (a *Adapter) NeedsAuth(src config.SourceConfig) bool { return true }

// Crawl will stream the tarball and emit pages. Not implemented yet.
func (a *Adapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (int, <-chan db.Page, error) {
    ch := make(chan db.Page)
    close(ch)
    return 0, ch, nil
}
```

- [ ] **Step 2.4: Add blank import in crawler**

In `internal/crawler/crawler.go`, within the import block near the top:

```go
import (
    // ... existing imports ...
    _ "github.com/documcp/documcp/internal/adapter/azuredevops"
    _ "github.com/documcp/documcp/internal/adapter/github"
    _ "github.com/documcp/documcp/internal/adapter/githubrepo"
    _ "github.com/documcp/documcp/internal/adapter/web"
)
```

- [ ] **Step 2.5: Run the registration test, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/
```

Expected: PASS.

- [ ] **Step 2.6: Commit**

```bash
git add internal/adapter/githubrepo/ internal/crawler/crawler.go
git commit -m "feat(adapter): register empty github_repo adapter"
```

---

## Task 3: Tarball streaming — happy path with allowlist filter

Implements the core of `Crawl`: fetch, gzip-decompress, tar-iterate, strip the `owner-repo-sha/` prefix, filter by extension allowlist, apply size limit, emit pages.

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo.go` (implement `Crawl`)
- Modify: `internal/adapter/githubrepo/githubrepo_test.go` (fixture helper + happy path test)

- [ ] **Step 3.1: Write the tarball fixture helper + happy-path test**

In `internal/adapter/githubrepo/githubrepo_test.go`, add:

```go
import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/documcp/documcp/internal/adapter/githubrepo"
    "github.com/documcp/documcp/internal/config"
    "github.com/documcp/documcp/internal/db"
)

// buildTarball produces a gzipped tar archive whose entries are prefixed
// with "owner-repo-sha/", mimicking GitHub's tarball output.
func buildTarball(t *testing.T, prefix string, entries map[string][]byte) []byte {
    t.Helper()
    var buf bytes.Buffer
    gz := gzip.NewWriter(&buf)
    tw := tar.NewWriter(gz)
    for name, data := range entries {
        hdr := &tar.Header{
            Name:     prefix + "/" + name,
            Mode:     0o644,
            Size:     int64(len(data)),
            Typeflag: tar.TypeReg,
        }
        if err := tw.WriteHeader(hdr); err != nil {
            t.Fatalf("write header: %v", err)
        }
        if _, err := tw.Write(data); err != nil {
            t.Fatalf("write body: %v", err)
        }
    }
    if err := tw.Close(); err != nil {
        t.Fatalf("close tar: %v", err)
    }
    if err := gz.Close(); err != nil {
        t.Fatalf("close gzip: %v", err)
    }
    return buf.Bytes()
}

// tarballServer returns an httptest.Server that serves the given tarball
// bytes for any /repos/.../tarball/... request.
func tarballServer(t *testing.T, tarball []byte) *httptest.Server {
    t.Helper()
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/x-gzip")
        _, _ = w.Write(tarball)
    }))
    t.Cleanup(srv.Close)
    return srv
}

func drainPages(ctx context.Context, t *testing.T, ch <-chan db.Page) []db.Page {
    t.Helper()
    out := make([]db.Page, 0)
    for p := range ch {
        out = append(out, p)
    }
    return out
}

func TestCrawl_HappyPath_WholeRepo(t *testing.T) {
    fiveMBPlus := bytes.Repeat([]byte("A"), 5*1024*1024+1)
    tarball := buildTarball(t, "owner-repo-abc123", map[string][]byte{
        "README.md":         []byte("# Project\n\nIntro."),
        "docs/guide.md":     []byte("# Guide\n\nBody."),
        "docs/api/auth.md":  []byte("# Auth\n\nBody."),
        "image.png":         []byte{0x89, 0x50, 0x4e, 0x47},
        "huge.md":           fiveMBPlus,
    })
    srv := tarballServer(t, tarball)

    a := githubrepo.NewAdapter(srv.URL)
    _, ch, err := a.Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "owner/repo",
        Branch: "main",
    }, 42)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    pages := drainPages(context.Background(), t, ch)

    if len(pages) != 3 {
        t.Fatalf("got %d pages, want 3", len(pages))
    }
    // Map by URL for stable assertions.
    byURL := make(map[string]db.Page, len(pages))
    for _, p := range pages {
        byURL[p.URL] = p
    }
    if _, ok := byURL["https://github.com/owner/repo/blob/main/README.md"]; !ok {
        t.Errorf("missing README.md page; got URLs %v", urls(pages))
    }
    if _, ok := byURL["https://github.com/owner/repo/blob/main/docs/guide.md"]; !ok {
        t.Errorf("missing docs/guide.md page; got URLs %v", urls(pages))
    }
    if _, ok := byURL["https://github.com/owner/repo/blob/main/docs/api/auth.md"]; !ok {
        t.Errorf("missing docs/api/auth.md page; got URLs %v", urls(pages))
    }
}

func urls(pages []db.Page) []string {
    out := make([]string, len(pages))
    for i, p := range pages {
        out[i] = p.URL
    }
    return out
}
```

- [ ] **Step 3.2: Run the test, expect FAIL**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_HappyPath_WholeRepo
```

Expected: `got 0 pages, want 3` (the skeleton returns an empty channel).

- [ ] **Step 3.3: Implement `Crawl`**

Replace the stub in `internal/adapter/githubrepo/githubrepo.go`:

```go
package githubrepo

import (
    "archive/tar"
    "compress/gzip"
    "context"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "net/url"
    "path"
    "strings"

    "github.com/documcp/documcp/internal/adapter"
    "github.com/documcp/documcp/internal/config"
    "github.com/documcp/documcp/internal/db"
)

const maxFileSize = 5 * 1024 * 1024 // 5 MiB per file

var allowedExts = map[string]struct{}{
    ".md":  {},
    ".mdx": {},
    ".txt": {},
}

func init() {
    adapter.Register("github_repo", NewAdapter("https://api.github.com"))
}

type Adapter struct{ baseURL string }

func NewAdapter(baseURL string) *Adapter {
    return &Adapter{baseURL: baseURL}
}

func (a *Adapter) NeedsAuth(src config.SourceConfig) bool { return true }

func (a *Adapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (int, <-chan db.Page, error) {
    branch := src.Branch
    if branch == "" {
        branch = "main"
    }
    includePath := normalizeIncludePath(src.IncludePath)

    tarURL := fmt.Sprintf("%s/repos/%s/tarball/%s", a.baseURL, src.Repo, url.PathEscape(branch))
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarURL, nil)
    if err != nil {
        return 0, nil, fmt.Errorf("github_repo: build request: %w", err)
    }
    req.Header.Set("Accept", "application/vnd.github+json")
    req.Header.Set("User-Agent", "documcp")
    if src.Token != "" {
        req.Header.Set("Authorization", "Bearer "+src.Token)
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return 0, nil, fmt.Errorf("github_repo: fetch tarball: %w", err)
    }
    if resp.StatusCode != http.StatusOK {
        resp.Body.Close()
        return 0, nil, fmt.Errorf("github_repo: tarball status %d for %s@%s", resp.StatusCode, src.Repo, branch)
    }

    ch := make(chan db.Page, 10)
    go func() {
        defer close(ch)
        defer resp.Body.Close()

        gz, err := gzip.NewReader(resp.Body)
        if err != nil {
            slog.Error("github_repo: gzip reader", "err", err)
            return
        }
        defer gz.Close()

        tr := tar.NewReader(gz)
        for {
            select {
            case <-ctx.Done():
                return
            default:
            }

            hdr, err := tr.Next()
            if err == io.EOF {
                return
            }
            if err != nil {
                slog.Error("github_repo: tar next", "err", err)
                return
            }
            if hdr.Typeflag != tar.TypeReg {
                continue
            }

            relPath, ok := stripRepoPrefix(hdr.Name)
            if !ok {
                continue
            }
            if includePath != "" && !strings.HasPrefix(relPath, includePath) {
                continue
            }
            if _, allowed := allowedExts[strings.ToLower(path.Ext(relPath))]; !allowed {
                continue
            }
            if hdr.Size > maxFileSize {
                slog.Warn("github_repo: file too large, skipping", "path", relPath, "size", hdr.Size)
                continue
            }

            content, err := io.ReadAll(io.LimitReader(tr, maxFileSize))
            if err != nil {
                slog.Warn("github_repo: read file", "path", relPath, "err", err)
                continue
            }

            page := buildPage(src.Repo, branch, includePath, relPath, string(content), sourceID)
            select {
            case <-ctx.Done():
                return
            case ch <- page:
            }
        }
    }()
    return 0, ch, nil
}

// stripRepoPrefix removes GitHub's "owner-repo-sha/" leading path segment
// from a tar entry name. Returns ok=false for entries without a prefix
// segment (which should not occur in real GitHub tarballs).
func stripRepoPrefix(name string) (string, bool) {
    idx := strings.IndexByte(name, '/')
    if idx < 0 {
        return "", false
    }
    rest := name[idx+1:]
    if rest == "" {
        return "", false
    }
    return rest, true
}

// normalizeIncludePath trims a leading slash and ensures a trailing slash
// on a non-empty prefix. An empty input is returned unchanged.
func normalizeIncludePath(p string) string {
    if p == "" {
        return ""
    }
    p = strings.TrimPrefix(p, "/")
    if !strings.HasSuffix(p, "/") {
        p += "/"
    }
    return p
}

// buildPage constructs a db.Page for a matched file.
func buildPage(repo, branch, includePath, relPath, content string, sourceID int64) db.Page {
    rel := strings.TrimPrefix(relPath, includePath)
    stem := strings.TrimSuffix(rel, path.Ext(rel))
    segments := strings.Split(stem, "/")
    // filter empty segments (defensive; shouldn't occur after TrimPrefix)
    pathSlice := make([]string, 0, len(segments))
    for _, s := range segments {
        if s != "" {
            pathSlice = append(pathSlice, s)
        }
    }
    return db.Page{
        SourceID: sourceID,
        URL:      fmt.Sprintf("https://github.com/%s/blob/%s/%s", repo, branch, relPath),
        Title:    filenameTitle(path.Base(rel)),
        Content:  content,
        Path:     pathSlice,
    }
}

// filenameTitle converts a filename like "getting-started.md" into
// "getting started" for fallback titles. Title extraction from content
// (H1 heading) is added in a later task.
func filenameTitle(name string) string {
    n := strings.TrimSuffix(name, path.Ext(name))
    n = strings.ReplaceAll(n, "-", " ")
    n = strings.ReplaceAll(n, "_", " ")
    return n
}
```

- [ ] **Step 3.4: Run the test, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_HappyPath_WholeRepo
```

Expected: PASS, 3 pages returned, oversized and non-Markdown skipped.

- [ ] **Step 3.5: Commit**

```bash
git add internal/adapter/githubrepo/
git commit -m "feat(github_repo): crawl repo tarball with extension and size filters"
```

---

## Task 4: Subfolder filter (`include_path`)

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 4.1: Write the failing test**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_IncludePath_ScopedToSubfolder(t *testing.T) {
    tarball := buildTarball(t, "owner-repo-abc123", map[string][]byte{
        "README.md":        []byte("# Root\n"),
        "docs/guide.md":    []byte("# Guide\n"),
        "docs/api/auth.md": []byte("# Auth\n"),
    })
    srv := tarballServer(t, tarball)

    a := githubrepo.NewAdapter(srv.URL)
    _, ch, err := a.Crawl(context.Background(), config.SourceConfig{
        Type:        "github_repo",
        Repo:        "owner/repo",
        Branch:      "main",
        IncludePath: "docs/",
    }, 42)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    pages := drainPages(context.Background(), t, ch)

    if len(pages) != 2 {
        t.Fatalf("got %d pages, want 2 (docs/guide.md, docs/api/auth.md)", len(pages))
    }

    byURL := make(map[string]db.Page, len(pages))
    for _, p := range pages {
        byURL[p.URL] = p
    }
    guide, ok := byURL["https://github.com/owner/repo/blob/main/docs/guide.md"]
    if !ok {
        t.Fatalf("guide page missing; got URLs %v", urls(pages))
    }
    if !equalStrings(guide.Path, []string{"guide"}) {
        t.Errorf("guide.Path: got %v, want [guide]", guide.Path)
    }
    auth, ok := byURL["https://github.com/owner/repo/blob/main/docs/api/auth.md"]
    if !ok {
        t.Fatalf("auth page missing; got URLs %v", urls(pages))
    }
    if !equalStrings(auth.Path, []string{"api", "auth"}) {
        t.Errorf("auth.Path: got %v, want [api auth]", auth.Path)
    }
}

func equalStrings(a, b []string) bool {
    if len(a) != len(b) {
        return false
    }
    for i := range a {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}
```

- [ ] **Step 4.2: Run the test, expect PASS**

Include-path filtering and `Path` derivation are already implemented in Task 3, so this test is a regression/behavior assertion rather than a new-feature driver.

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_IncludePath_ScopedToSubfolder
```

Expected: PASS.

- [ ] **Step 4.3: Commit**

```bash
git add internal/adapter/githubrepo/githubrepo_test.go
git commit -m "test(github_repo): cover include_path subfolder scoping"
```

---

## Task 5: Title extraction from first H1 (fallback to filename)

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo.go` (add `extractTitle`, use in `buildPage`)
- Modify: `internal/adapter/githubrepo/githubrepo_test.go` (add title tests)

- [ ] **Step 5.1: Write failing tests**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_Title_FromFirstH1(t *testing.T) {
    content := []byte("Some preamble\n\n# Real Title\n\nBody text.\n")
    tarball := buildTarball(t, "owner-repo-abc123", map[string][]byte{
        "doc.md": content,
    })
    srv := tarballServer(t, tarball)

    _, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    pages := drainPages(context.Background(), t, ch)
    if len(pages) != 1 {
        t.Fatalf("got %d pages, want 1", len(pages))
    }
    if pages[0].Title != "Real Title" {
        t.Errorf("Title: got %q, want %q", pages[0].Title, "Real Title")
    }
}

func TestCrawl_Title_FallsBackToFilename(t *testing.T) {
    tarball := buildTarball(t, "owner-repo-abc123", map[string][]byte{
        "getting-started.md": []byte("No heading here, just body."),
        "notes.txt":          []byte("# Not a Markdown heading in a txt file"),
    })
    srv := tarballServer(t, tarball)

    _, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    pages := drainPages(context.Background(), t, ch)

    byURL := make(map[string]db.Page, len(pages))
    for _, p := range pages {
        byURL[p.URL] = p
    }
    md := byURL["https://github.com/o/r/blob/main/getting-started.md"]
    if md.Title != "getting started" {
        t.Errorf(".md fallback title: got %q, want %q", md.Title, "getting started")
    }
    txt := byURL["https://github.com/o/r/blob/main/notes.txt"]
    if txt.Title != "notes" {
        t.Errorf(".txt title (never uses H1 parse): got %q, want %q", txt.Title, "notes")
    }
}
```

- [ ] **Step 5.2: Run tests, expect FAIL**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_Title
```

Expected: H1 test fails (current code always uses filename).

- [ ] **Step 5.3: Add `extractTitle` and wire into `buildPage`**

In `internal/adapter/githubrepo/githubrepo.go`:

```go
// extractTitle returns the text of the first H1 heading in Markdown content.
// Returns "" if no H1 is present. The content must be .md or .mdx; callers
// pass "" for .txt files.
func extractTitle(content string) string {
    for _, line := range strings.Split(content, "\n") {
        trimmed := strings.TrimLeft(line, " \t")
        if strings.HasPrefix(trimmed, "# ") {
            return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
        }
    }
    return ""
}
```

Update `buildPage` to use it:

```go
func buildPage(repo, branch, includePath, relPath, content string, sourceID int64) db.Page {
    rel := strings.TrimPrefix(relPath, includePath)
    stem := strings.TrimSuffix(rel, path.Ext(rel))
    segments := strings.Split(stem, "/")
    pathSlice := make([]string, 0, len(segments))
    for _, s := range segments {
        if s != "" {
            pathSlice = append(pathSlice, s)
        }
    }

    title := ""
    ext := strings.ToLower(path.Ext(rel))
    if ext == ".md" || ext == ".mdx" {
        title = extractTitle(content)
    }
    if title == "" {
        title = filenameTitle(path.Base(rel))
    }

    return db.Page{
        SourceID: sourceID,
        URL:      fmt.Sprintf("https://github.com/%s/blob/%s/%s", repo, branch, relPath),
        Title:    title,
        Content:  content,
        Path:     pathSlice,
    }
}
```

- [ ] **Step 5.4: Run tests, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/
```

Expected: all adapter tests PASS.

- [ ] **Step 5.5: Commit**

```bash
git add internal/adapter/githubrepo/
git commit -m "feat(github_repo): extract title from first H1, fall back to filename"
```

---

## Task 6: Auth header forwarding + cross-host redirect drop

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 6.1: Write failing tests**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_SendsAuthHeader_WhenTokenSet(t *testing.T) {
    tarball := buildTarball(t, "o-r-sha", map[string][]byte{"README.md": []byte("# R\n")})

    var gotAuth string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotAuth = r.Header.Get("Authorization")
        w.Header().Set("Content-Type", "application/x-gzip")
        _, _ = w.Write(tarball)
    }))
    t.Cleanup(srv.Close)

    _, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
        Token:  "tok123",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    _ = drainPages(context.Background(), t, ch)

    if gotAuth != "Bearer tok123" {
        t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer tok123")
    }
}

func TestCrawl_OmitsAuthHeader_WhenTokenEmpty(t *testing.T) {
    tarball := buildTarball(t, "o-r-sha", map[string][]byte{"README.md": []byte("# R\n")})

    var gotAuth string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotAuth = r.Header.Get("Authorization")
        w.Header().Set("Content-Type", "application/x-gzip")
        _, _ = w.Write(tarball)
    }))
    t.Cleanup(srv.Close)

    _, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    _ = drainPages(context.Background(), t, ch)

    if gotAuth != "" {
        t.Errorf("Authorization: got %q, want empty", gotAuth)
    }
}
```

- [ ] **Step 6.2: Run tests, expect PASS**

Auth handling was implemented in Task 3; these tests validate that behavior.

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_.*AuthHeader
```

Expected: PASS.

- [ ] **Step 6.3: Commit**

```bash
git add internal/adapter/githubrepo/githubrepo_test.go
git commit -m "test(github_repo): assert auth header is forwarded when token set"
```

---

## Task 7: Error mapping for 401/403/404

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo.go` (map status codes to specific error messages)
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 7.1: Write failing tests**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_404_ReturnsNotFoundError(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNotFound)
    }))
    t.Cleanup(srv.Close)

    _, _, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "ghost/repo",
        Branch: "main",
    }, 1)
    if err == nil {
        t.Fatal("expected error, got nil")
    }
    if !strings.Contains(err.Error(), "ghost/repo") || !strings.Contains(err.Error(), "main") {
        t.Errorf("error should mention repo and branch: %v", err)
    }
    if !strings.Contains(err.Error(), "not found") {
        t.Errorf("error should say 'not found': %v", err)
    }
}

func TestCrawl_401_ReturnsUnauthorizedError(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusUnauthorized)
    }))
    t.Cleanup(srv.Close)

    _, _, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
    }, 1)
    if err == nil {
        t.Fatal("expected error, got nil")
    }
    if !strings.Contains(err.Error(), "unauthorized") {
        t.Errorf("error should say 'unauthorized': %v", err)
    }
}
```

Also add `"strings"` to the test file imports if not already present.

- [ ] **Step 7.2: Run tests, expect FAIL**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_404_ReturnsNotFoundError
```

Expected: current generic `tarball status %d` error does not include the words "not found" or "unauthorized".

- [ ] **Step 7.3: Replace the generic status check with explicit mappings**

In `internal/adapter/githubrepo/githubrepo.go`, replace the single `if resp.StatusCode != http.StatusOK` block with:

```go
    switch resp.StatusCode {
    case http.StatusOK:
        // success, continue
    case http.StatusNotFound:
        resp.Body.Close()
        return 0, nil, fmt.Errorf("github_repo: repo or branch not found: %s@%s", src.Repo, branch)
    case http.StatusUnauthorized, http.StatusForbidden:
        resp.Body.Close()
        return 0, nil, fmt.Errorf("github_repo: unauthorized — token missing or lacks repo scope (status %d)", resp.StatusCode)
    default:
        resp.Body.Close()
        return 0, nil, fmt.Errorf("github_repo: tarball status %d for %s@%s", resp.StatusCode, src.Repo, branch)
    }
```

- [ ] **Step 7.4: Run tests, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/
```

Expected: all PASS.

- [ ] **Step 7.5: Commit**

```bash
git add internal/adapter/githubrepo/
git commit -m "feat(github_repo): map 401/403/404 to actionable error messages"
```

---

## Task 8: 429 retry with `Retry-After`

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo.go` (wrap the initial fetch in a single-retry loop)
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 8.1: Write failing test**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_429_RetriesOnce(t *testing.T) {
    tarball := buildTarball(t, "o-r-sha", map[string][]byte{"README.md": []byte("# R\n")})

    var hits int
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        hits++
        if hits == 1 {
            w.Header().Set("Retry-After", "0")
            w.WriteHeader(http.StatusTooManyRequests)
            return
        }
        w.Header().Set("Content-Type", "application/x-gzip")
        _, _ = w.Write(tarball)
    }))
    t.Cleanup(srv.Close)

    _, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }
    pages := drainPages(context.Background(), t, ch)
    if hits != 2 {
        t.Errorf("expected 2 server hits (1 retry), got %d", hits)
    }
    if len(pages) != 1 {
        t.Errorf("expected 1 page after retry, got %d", len(pages))
    }
}
```

- [ ] **Step 8.2: Run the test, expect FAIL**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_429_RetriesOnce
```

Expected: current code treats 429 as a generic non-OK status — 1 hit, no retry, error returned.

- [ ] **Step 8.3: Add retry logic**

Factor the fetch into a helper `fetchTarball`:

```go
func (a *Adapter) fetchTarball(ctx context.Context, src config.SourceConfig, branch string) (*http.Response, error) {
    tarURL := fmt.Sprintf("%s/repos/%s/tarball/%s", a.baseURL, src.Repo, url.PathEscape(branch))

    var resp *http.Response
    for attempt := 0; attempt < 2; attempt++ {
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarURL, nil)
        if err != nil {
            return nil, fmt.Errorf("github_repo: build request: %w", err)
        }
        req.Header.Set("Accept", "application/vnd.github+json")
        req.Header.Set("User-Agent", "documcp")
        if src.Token != "" {
            req.Header.Set("Authorization", "Bearer "+src.Token)
        }

        resp, err = http.DefaultClient.Do(req)
        if err != nil {
            return nil, fmt.Errorf("github_repo: fetch tarball: %w", err)
        }
        if resp.StatusCode != http.StatusTooManyRequests || attempt == 1 {
            return resp, nil
        }

        retryAfter := parseRetryAfterSeconds(resp.Header.Get("Retry-After"))
        resp.Body.Close()
        if retryAfter > 60 {
            retryAfter = 60
        }
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(time.Duration(retryAfter) * time.Second):
        }
    }
    return resp, nil
}

// parseRetryAfterSeconds parses the Retry-After header. Only the seconds
// integer form is honored (HTTP-date form is uncommon for GitHub). Returns
// 0 on parse failure so tests setting "0" work and retries don't stall.
func parseRetryAfterSeconds(v string) int {
    if v == "" {
        return 0
    }
    n, err := strconv.Atoi(strings.TrimSpace(v))
    if err != nil || n < 0 {
        return 0
    }
    return n
}
```

Add `"strconv"` and `"time"` to the imports. In `Crawl`, replace the direct `http.DefaultClient.Do(req)` call with `resp, err := a.fetchTarball(ctx, src, branch)`.

- [ ] **Step 8.4: Run tests, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/
```

Expected: all PASS, including `TestCrawl_429_RetriesOnce`.

- [ ] **Step 8.5: Commit**

```bash
git add internal/adapter/githubrepo/
git commit -m "feat(github_repo): retry once on 429 using Retry-After"
```

---

## Task 9: Context cancellation

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 9.1: Write the test**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_ContextCancellation_ClosesChannel(t *testing.T) {
    // Large tarball to ensure streaming is in progress when we cancel.
    entries := make(map[string][]byte, 50)
    for i := 0; i < 50; i++ {
        entries[fmt.Sprintf("doc%d.md", i)] = []byte("# T\n\nbody.")
    }
    tarball := buildTarball(t, "o-r-sha", entries)
    srv := tarballServer(t, tarball)

    ctx, cancel := context.WithCancel(context.Background())
    _, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(ctx, config.SourceConfig{
        Type:   "github_repo",
        Repo:   "o/r",
        Branch: "main",
    }, 1)
    if err != nil {
        t.Fatalf("Crawl: %v", err)
    }

    // Receive one page, then cancel and drain.
    <-ch
    cancel()

    // Drain remaining pages; the goroutine must exit and close the channel.
    done := make(chan struct{})
    go func() {
        for range ch {
        }
        close(done)
    }()
    select {
    case <-done:
        // channel closed, success
    case <-time.After(5 * time.Second):
        t.Fatal("Crawl goroutine did not exit within 5s of cancel")
    }
}
```

Add `"time"` to the test imports if not already present.

- [ ] **Step 9.2: Run test, expect PASS**

Cancellation handling was implemented in Task 3. This test validates it.

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_ContextCancellation
```

Expected: PASS.

- [ ] **Step 9.3: Commit**

```bash
git add internal/adapter/githubrepo/githubrepo_test.go
git commit -m "test(github_repo): cover context cancellation mid-stream"
```

---

## Task 10: Reject path-traversal `include_path`

**Files:**
- Modify: `internal/adapter/githubrepo/githubrepo.go` (validate before fetching)
- Modify: `internal/adapter/githubrepo/githubrepo_test.go`

- [ ] **Step 10.1: Write the failing test**

Append to `internal/adapter/githubrepo/githubrepo_test.go`:

```go
func TestCrawl_IncludePath_RejectsTraversal(t *testing.T) {
    a := githubrepo.NewAdapter("http://127.0.0.1:0") // URL unused — should error before HTTP

    _, _, err := a.Crawl(context.Background(), config.SourceConfig{
        Type:        "github_repo",
        Repo:        "o/r",
        Branch:      "main",
        IncludePath: "../secrets",
    }, 1)
    if err == nil {
        t.Fatal("expected error for traversal include_path, got nil")
    }
    if !strings.Contains(err.Error(), "include_path") {
        t.Errorf("error should mention include_path: %v", err)
    }
}
```

- [ ] **Step 10.2: Run test, expect FAIL**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/ -run TestCrawl_IncludePath_RejectsTraversal
```

Expected: current code passes traversal through to the HTTP call and returns a network/status error, not the specific include_path error.

- [ ] **Step 10.3: Add validation early in `Crawl`**

In `internal/adapter/githubrepo/githubrepo.go` `Crawl`, insert after the `branch`/`includePath` normalization and before the `fetchTarball` call:

```go
    if err := validateIncludePath(src.IncludePath); err != nil {
        return 0, nil, err
    }
```

Add the helper:

```go
// validateIncludePath rejects include_path values that would escape the
// repo root. Must be called with the raw (un-normalized) value.
func validateIncludePath(raw string) error {
    if raw == "" {
        return nil
    }
    clean := path.Clean(strings.TrimPrefix(raw, "/"))
    if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
        return fmt.Errorf("github_repo: invalid include_path %q (must not contain '..' segments)", raw)
    }
    return nil
}
```

- [ ] **Step 10.4: Run tests, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/adapter/githubrepo/
```

Expected: all PASS.

- [ ] **Step 10.5: Commit**

```bash
git add internal/adapter/githubrepo/
git commit -m "feat(github_repo): reject include_path with '..' traversal"
```

---

## Task 11: API — extend `authStart` to treat `github_repo` as a GitHub flow

**Files:**
- Modify: `internal/api/handlers.go:211`
- Modify: `internal/api/handlers_test.go`

- [ ] **Step 11.1: Write failing test**

Inspect `internal/api/handlers_test.go` around the existing GitHub-flow auth-start test (near line 342 per earlier grep) and add a sibling test:

```go
func TestAuthStart_github_repo_starts_github_device_flow(t *testing.T) {
    // Mirror the setup of the existing github_wiki authStart test. If the
    // existing test uses a helper like newTestServer(t) or a mock GitHub
    // device-code endpoint, reuse it here.
    store := openTestStore(t)
    defer store.Close()

    id, err := store.InsertSource(db.Source{
        Name: "Repo", Type: "github_repo", Repo: "owner/example", Branch: "main",
    })
    if err != nil {
        t.Fatalf("InsertSource: %v", err)
    }

    // Replace this block with whatever helper the existing github_wiki
    // authStart test uses to stand up an API server with a mocked GitHub
    // device-code endpoint, then issue POST /api/sources/{id}/auth/start
    // and assert the response includes user_code and verification_uri.
    // The behavior under test: the handler must take the github_wiki branch
    // (GitHub flow), NOT the else branch (Microsoft flow), when
    // src.Type == "github_repo".
}
```

Before running, read `internal/api/handlers_test.go` and copy the exact helpers and assertions from the existing `github_wiki` device-flow test so this new test shape matches.

- [ ] **Step 11.2: Run the test, expect FAIL**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/api/ -run TestAuthStart_github_repo
```

Expected: the handler routes `github_repo` to the Microsoft branch, so the user-code / verification-uri fields are sourced from a Microsoft mock and do not match GitHub expectations.

- [ ] **Step 11.3: Update the handler condition**

In `internal/api/handlers.go` around line 211:

```go
    if src.Type == "github_wiki" || src.Type == "github_repo" {
        clientID := githubClientID()
        ghFlow, err := auth.NewGitHubDeviceFlow("https://github.com", clientID)
        // ... rest unchanged ...
    } else {
        // Microsoft device flow
    }
```

- [ ] **Step 11.4: Run tests, expect PASS**

```bash
CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/api/
```

Expected: all PASS, including existing `github_wiki` / `azure_devops` auth-start tests.

- [ ] **Step 11.5: Commit**

```bash
git add internal/api/
git commit -m "feat(api): route github_repo auth through GitHub device flow"
```

---

## Task 12: Web UI — add source-type option, branch input, `include_path` input

**Files:**
- Modify: `web/static/index.html:29-47`
- Modify: `web/static/app.js:154`

- [ ] **Step 12.1: Add option, Repo/Branch/IncludePath inputs to form**

In `web/static/index.html`, update the Add-Source form section:

```html
<select x-model="newSource.Type">
  <option value="web">Web</option>
  <option value="github_wiki">GitHub Wiki</option>
  <option value="github_repo">GitHub Repo</option>
  <option value="azure_devops">Azure DevOps Wiki</option>
</select>
<input type="text" x-model="newSource.Name" placeholder="Source name" />
<template x-if="newSource.Type === 'web'">
  <input type="url" x-model="newSource.URL" placeholder="https://docs.example.com" />
</template>
<template x-if="newSource.Type === 'web'">
  <input type="url" x-model="newSource.IncludePath"
         placeholder="Restrict to path prefix (optional, e.g. https://docs.example.com/guide/)" />
</template>
<template x-if="newSource.Type === 'github_wiki'">
  <input type="text" x-model="newSource.Repo" placeholder="owner/repo" />
</template>
<template x-if="newSource.Type === 'github_repo'">
  <input type="text" x-model="newSource.Repo" placeholder="owner/repo" />
</template>
<template x-if="newSource.Type === 'github_repo'">
  <input type="text" x-model="newSource.Branch" placeholder="Branch (default: main)" />
</template>
<template x-if="newSource.Type === 'github_repo'">
  <input type="text" x-model="newSource.IncludePath"
         placeholder="Subfolder prefix (optional, e.g. docs/)" />
</template>
<template x-if="newSource.Type === 'azure_devops'">
  <input type="url" x-model="newSource.URL" placeholder="https://dev.azure.com/org/project" />
</template>
<input type="text" x-model="newSource.CrawlSchedule" placeholder="Cron schedule (e.g. 0 2 * * *)" />
```

- [ ] **Step 12.2: Add label to `sourceTypeName` map**

In `web/static/app.js` around line 154, extend the map:

```javascript
const map = { web: 'Web', github_wiki: 'GitHub Wiki', github_repo: 'GitHub Repo', azure_devops: 'Azure DevOps' }
```

- [ ] **Step 12.3: Manual UI smoke**

Build and run the container, then open the Web UI:

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/documcp ./cmd/documcp
make docker
make run
```

Open `http://localhost:8080`, click **+ Add Source**, select **GitHub Repo**, verify the form shows three inputs: `owner/repo`, branch, subfolder. Close the form (do not submit — end-to-end submission is covered in Task 13).

- [ ] **Step 12.4: Commit**

```bash
git add web/static/
git commit -m "feat(ui): github_repo source type option with branch and subfolder inputs"
```

---

## Task 13: End-to-end smoke test (manual)

**Files:** none — validation only.

This task exercises the full stack against real GitHub.

- [ ] **Step 13.1: Rebuild and run**

```bash
make docker && make run
```

- [ ] **Step 13.2: Add a public-repo source via the UI**

Open `http://localhost:8080`, click **+ Add Source**, pick **GitHub Repo**. Use a small public repo with `.md` docs in a subfolder — for example:
- Repo: `kubernetes/community`
- Branch: `main`
- Subfolder: `contributors/guide/`

Click **Add**, then click **Crawl Now** on the new source.

- [ ] **Step 13.3: Verify crawl completes**

Watch the page-count badge on the sources card — it should tick upward and then stop crawling. Confirm `LastCrawled` appears.

- [ ] **Step 13.4: Verify browse and search**

- Run `browse_source` via the MCP endpoint or the Web UI browse view: pages should reflect the subfolder hierarchy (folder names as path segments, no `contributors/guide/` prefix visible, since it was the `include_path`).
- Run `search_docs` for a term you know is in one of those docs; confirm you get results with clickable URLs that deep-link into `https://github.com/kubernetes/community/blob/main/contributors/guide/...`.

- [ ] **Step 13.5: (Optional) verify private-repo auth**

Add a private repo you own as a second source. Click **Connect** to start the GitHub device flow. Enter the user code at github.com, wait for completion, then **Crawl Now**. Confirm pages are indexed. If you see a `401 unauthorized` error surface in the UI instead, the token scope is wrong — the existing GitHub device flow in `internal/auth/github_auth.go` requests the `repo` scope.

- [ ] **Step 13.6: Commit a note if anything surprised you**

If manual testing surfaces a real issue, add a follow-up task to this plan or open a new commit with the fix. Otherwise, no commit needed for this task.

---

## Self-Review

Ran against the spec (`docs/plans/2026-04-16-github-repo-source-design.md`):

| Spec requirement | Task(s) |
|---|---|
| New `github_repo` adapter in dedicated package | Task 2, 3 |
| Reuse GitHub device-code auth | Tasks 1 (providerForType), 11 (authStart) |
| `branch` config field with `main` default | Task 1 |
| `include_path` subfolder scope (reuse existing field) | Task 4 |
| One tarball request per crawl via `archive/tar` streaming | Task 3 |
| Extension allowlist `.md` / `.mdx` / `.txt` | Task 3 |
| 5 MiB per-file size cap | Task 3 |
| Title from first H1, fallback to filename (always for `.txt`) | Task 5 |
| `URL` = `https://github.com/{repo}/blob/{branch}/{path}` | Task 3 |
| `Path` = folder-segments-after-include-path | Tasks 3, 4 |
| Return `(0, ch, nil)` | Task 3 |
| Error mapping: 401/403, 404 | Task 7 |
| Error mapping: 429 with Retry-After | Task 8 |
| Network/5xx, gzip/tar decode errors | Task 3 (error-return path and slog inside goroutine) |
| Context cancellation | Tasks 3, 9 |
| Reject `..` in `include_path` | Task 10 |
| Schema migration, CRUD, `sourceToConfig` | Task 1 |
| Web UI form option, branch + include_path inputs | Task 12 |
| Tests mirror `github/github_test.go` with `httptest.Server` + `NewAdapter(baseURL)` | Tasks 3–10 |
| End-to-end validation | Task 13 |

**Placeholder scan:** No TBDs or "fill in later". Task 11 (API auth-start test) intentionally asks the implementer to read the existing `github_wiki` authStart test and mirror its helpers — this is a deliberate lookup, not a placeholder, because the existing test's helper pattern is not reproduced in the spec and duplicating it here would desync on any future refactor.

**Type consistency:** `Adapter`, `NewAdapter`, `extractTitle`, `filenameTitle`, `buildPage`, `stripRepoPrefix`, `normalizeIncludePath`, `validateIncludePath`, `fetchTarball`, `parseRetryAfterSeconds` — all introduced exactly once and used consistently across tasks. `SourceConfig.Branch` / `db.Source.Branch` referenced in the same form throughout. `include_path` validation uses the un-normalized input in Task 10 and the normalized form in Task 3 — that split is intentional (validate the user's input before normalization).

**Scope check:** One adapter, one set of storage changes, one handler condition, one UI form change. Single plan.

**Spec vs. plan note:** The spec says `include_path` is "rejected at config load". The plan rejects at `Crawl`-time (Task 10) to match the established pattern (the web adapter validates its `IncludePath` in `Crawl`). Effect is the same — the crawl fails before any external request is made. Spec will be updated to match in a follow-up to keep the two documents consistent.
