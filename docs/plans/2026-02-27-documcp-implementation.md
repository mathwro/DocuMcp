# DocuMcp Implementation Plan

> **Historical record — not authoritative.** This is the original task plan from 2026-02-27. All 27 tasks shipped (see `README.md`); subsequent changes are in `docs/plans/2026-*` files. Some original acceptance criteria no longer match shipped behavior — notably "Adding a public web source in UI writes to config.yaml" (the UI persists to SQLite instead) and the env-var defaults around `DOCUMCP_CONFIG` (now optional). Refer to current code and `docs/configuration.md` for the live contract.

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

## Progress

| Task | Status | Commit |
|------|--------|--------|
| Task 1: Go module + project structure | ✅ Done | `4da1b79`, `14c3e4f` |
| Task 2: Config types + YAML loading | ✅ Done | `35d4978`, `d130a82` |
| Task 3: Config file watcher | ✅ Done | `9a24347`, `ce49c1d` |
| Task 4: SQLite database + schema | ✅ Done | `a631a05`, `d218a4f` |
| Task 5: FTS5 full-text search | ✅ Done | `d45d5e2`, `88ae25f` |
| Task 6: ONNX embedding wrapper | ✅ Done | `b036167`, `46fb47c` |
| Task 7: sqlite-vec + vector search + RRF | ✅ Done | `b1f1926`, `a8e76a0` |
| Task 8: Hierarchical browse (TOC) | ✅ Done | `51a0384` |
| Task 9: Adapter interface + registry | ✅ Done | `cac558e` |
| Task 10: Web adapter — sitemap parsing | ✅ Done | `3aa49b9`, `fad530c` |
| Task 11: Web adapter — HTML extraction + crawling | ✅ Done | `1ad2237` |
| Task 12: GitHub Wiki adapter | ✅ Done | `3bea0c3` |
| Task 13: Azure DevOps Wiki adapter | ✅ Done | `a364538` |
| Task 14: Microsoft device code flow | ✅ Done | `041b13d` |
| Task 15: GitHub device code flow | ✅ Done | `5427187` |
| Task 16: Encrypted token storage | ✅ Done | `53a7ade` |
| Task 17: Crawl orchestrator | ✅ Done | `aa95cb7`, `61d8670` |
| Task 18: Cron scheduler | ✅ Done | `f7beb6b`, `5a3b974` |
| Task 19: MCP server (4 tools) | ✅ Done | `7130831`, `6f07ceb`, `d123593` |
| Task 20: REST API for Web UI | ✅ Done | `4d86ce5`, `db44fab` |
| Task 21: Auth flow REST endpoints | ✅ Done | `58aeab1`, `f2f801f` |
| Task 22: Web UI base layout + dark theme | ✅ Done | `a62e7bc` |
| Task 23: Sources dashboard | ✅ Done | `a62e7bc` |
| Task 24: Search debug UI | ✅ Done | `a62e7bc`, `f86e74b` |
| Task 25: Wire main binary | ✅ Done | `8157541`, `87a52c9` |
| Task 26: Dockerfile + docker-compose | ✅ Done | `da2b698`, `e002a49` |
| Task 27: Makefile targets | ✅ Done | `1196273`, `4a08cff` |

**Build notes (discovered during implementation):**
- Use `CGO_ENABLED=1 go build -tags sqlite_fts5` — FTS5 requires the `sqlite_fts5` build tag
- Go version on dev machine: 1.26.0
- Makefile already has the correct flags

**Key patterns established:**
- Error wrapping: `fmt.Errorf("context: %w", err)` throughout
- `db.ErrNotFound` sentinel wraps `sql.ErrNoRows` — callers use `errors.Is`
- FTS5 index kept in sync via SQL triggers (pages_ai, pages_au, pages_ad)
- `ListSources` returns `make([]Source, 0)` (not nil) for empty results

---

**Goal:** Build a locally-hosted MCP server in Go that indexes documentation from GitHub wikis, Azure DevOps, Confluence, and generic web sources, and exposes them to AI coding assistants via hybrid full-text + semantic search and hierarchical browsing.

**Architecture:** Single Go binary with embedded web UI, SQLite (FTS5 + sqlite-vec) for storage, bundled all-MiniLM-L6-v2 ONNX model for embeddings, and the official MCP Go SDK for the MCP server. Source adapters implement a common interface, auth uses device code flows (no user app registration required).

**Tech Stack:** Go 1.23+, `github.com/modelcontextprotocol/go-sdk` v1.4.0, `github.com/mattn/go-sqlite3` (CGo), `github.com/asg017/sqlite-vec-go-bindings`, `github.com/knights-analytics/hugot` (ONNX), `gopkg.in/yaml.v3`, `github.com/robfig/cron/v3`, `github.com/fsnotify/fsnotify`, Alpine.js for Web UI

---

## Directory Structure

```
DocuMcp/
├── cmd/documcp/main.go
├── internal/
│   ├── config/         # YAML config loading + file watcher
│   ├── db/             # SQLite schema, migrations, CRUD
│   ├── search/         # FTS5, semantic search, RRF merging, browse
│   ├── embed/          # ONNX embedding model wrapper
│   ├── adapter/        # Adapter interface + implementations
│   │   ├── web/        # Generic web + Azure AD
│   │   ├── github/     # GitHub Wiki via REST API
│   │   └── azuredevops/
│   ├── auth/           # Device code flows, token storage
│   ├── crawler/        # Crawl orchestration + cron scheduler
│   ├── mcp/            # MCP server + tool handlers
│   └── api/            # REST API for Web UI
├── web/                # Embedded static files (HTML/JS/CSS)
├── models/             # ONNX model files (downloaded at build time)
├── docs/plans/
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

---

## Phase 1: Foundation

### Task 1: Initialize Go module and project structure

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `cmd/documcp/main.go`

**Step 1: Initialize module**

```bash
go mod init github.com/documcp/documcp
```
Expected: `go.mod` created with `module github.com/documcp/documcp`

**Step 2: Create directory structure**

```bash
mkdir -p cmd/documcp internal/{config,db,search,embed,adapter/{web,github,azuredevops},auth,crawler,mcp,api} web/static models
```

**Step 3: Create minimal main.go**

```go
// cmd/documcp/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "DocuMcp starting...")
	os.Exit(0)
}
```

**Step 4: Create Makefile**

```makefile
.PHONY: build test lint clean

build:
	CGO_ENABLED=1 go build -o bin/documcp ./cmd/documcp

test:
	CGO_ENABLED=1 go test ./... -v

lint:
	golangci-lint run

clean:
	rm -rf bin/
```

**Step 5: Verify build**

```bash
make build
```
Expected: `bin/documcp` created, no errors

**Step 6: Commit**

```bash
git add .
git commit -m "feat: initialize go module and project structure"
```

---

### Task 2: Configuration types and YAML loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/config/testdata/valid.yaml`

**Step 1: Add YAML dependency**

```bash
go get gopkg.in/yaml.v3
```

**Step 2: Write the failing test**

```go
// internal/config/config_test.go
package config_test

import (
	"testing"
	"github.com/documcp/documcp/internal/config"
)

func TestLoadConfig_ValidFile(t *testing.T) {
	cfg, err := config.Load("testdata/valid.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(cfg.Sources))
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := config.Load("testdata/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadConfig_SourceTypes(t *testing.T) {
	cfg, _ := config.Load("testdata/valid.yaml")
	types := map[string]bool{}
	for _, s := range cfg.Sources {
		types[s.Type] = true
	}
	if !types["github_wiki"] {
		t.Error("expected github_wiki source type")
	}
	if !types["web"] {
		t.Error("expected web source type")
	}
}
```

**Step 3: Run test to verify it fails**

```bash
CGO_ENABLED=1 go test ./internal/config/... -v
```
Expected: FAIL — package not found / type not defined

**Step 4: Create testdata**

```yaml
# internal/config/testdata/valid.yaml
server:
  port: 8080
  data_dir: /tmp/documcp-test

sources:
  - name: "Test GitHub Wiki"
    type: github_wiki
    repo: "myorg/myrepo"
    crawl_schedule: "0 0 * * 1"

  - name: "Test Internal Docs"
    type: web
    url: "https://docs.example.com"
    auth: azure_ad
    crawl_schedule: "0 */6 * * *"
```

**Step 5: Implement config types**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig   `yaml:"server"`
	Sources []SourceConfig `yaml:"sources"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	DataDir string `yaml:"data_dir"`
}

type SourceConfig struct {
	Name          string `yaml:"name"`
	Type          string `yaml:"type"`
	// github_wiki fields
	Repo          string `yaml:"repo,omitempty"`
	// web fields
	URL           string `yaml:"url,omitempty"`
	Auth          string `yaml:"auth,omitempty"`
	// confluence fields
	BaseURL       string `yaml:"base_url,omitempty"`
	SpaceKey      string `yaml:"space_key,omitempty"`
	// scheduling
	CrawlSchedule string `yaml:"crawl_schedule,omitempty"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.DataDir == "" {
		cfg.Server.DataDir = "/app/data"
	}
	return &cfg, nil
}
```

**Step 6: Run tests to verify pass**

```bash
CGO_ENABLED=1 go test ./internal/config/... -v
```
Expected: PASS

**Step 7: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: add config types and YAML loading"
```

---

### Task 3: Config file watcher

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/watcher_test.go`

**Step 1: Add fsnotify**

```bash
go get github.com/fsnotify/fsnotify
```

**Step 2: Write failing test**

```go
// internal/config/watcher_test.go
package config_test

import (
	"os"
	"testing"
	"time"
	"github.com/documcp/documcp/internal/config"
)

func TestWatcher_CallsCallbackOnChange(t *testing.T) {
	f, _ := os.CreateTemp("", "config-*.yaml")
	f.WriteString("server:\n  port: 8080\nsources: []\n")
	f.Close()
	defer os.Remove(f.Name())

	called := make(chan struct{}, 1)
	w, err := config.Watch(f.Name(), func(cfg *config.Config) {
		called <- struct{}{}
	})
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	defer w.Stop()

	os.WriteFile(f.Name(), []byte("server:\n  port: 9090\nsources: []\n"), 0644)

	select {
	case <-called:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("callback not called after file change")
	}
}
```

**Step 3: Run test to verify fails**

```bash
CGO_ENABLED=1 go test ./internal/config/... -run TestWatcher -v
```
Expected: FAIL — Watch not defined

**Step 4: Implement watcher**

```go
// Add to internal/config/config.go

import "github.com/fsnotify/fsnotify"

type Watcher struct {
	fsw *fsnotify.Watcher
}

func Watch(path string, onChange func(*Config)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(path); err != nil {
		fsw.Close()
		return nil, err
	}
	go func() {
		for range fsw.Events {
			cfg, err := Load(path)
			if err == nil {
				onChange(cfg)
			}
		}
	}()
	return &Watcher{fsw: fsw}, nil
}

func (w *Watcher) Stop() { w.fsw.Close() }
```

**Step 5: Run test to verify passes**

```bash
CGO_ENABLED=1 go test ./internal/config/... -v
```
Expected: PASS

**Step 6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: add config file watcher"
```

---

### Task 4: SQLite database and schema

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/schema.go`
- Create: `internal/db/db_test.go`

**Step 1: Add SQLite dependency**

```bash
go get github.com/mattn/go-sqlite3
```

**Step 2: Write failing tests**

```go
// internal/db/db_test.go
package db_test

import (
	"testing"
	"github.com/documcp/documcp/internal/db"
)

func TestOpen_CreatesSchema(t *testing.T) {
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
}

func TestInsertAndGetSource(t *testing.T) {
	store, _ := db.Open(":memory:")
	defer store.Close()

	id, err := store.InsertSource(db.Source{
		Name: "Test Source",
		Type: "web",
		URL:  "https://example.com",
	})
	if err != nil {
		t.Fatalf("InsertSource: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero id")
	}

	sources, err := store.ListSources()
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "Test Source" {
		t.Errorf("unexpected sources: %+v", sources)
	}
}

func TestInsertAndGetPage(t *testing.T) {
	store, _ := db.Open(":memory:")
	defer store.Close()

	sourceID, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	err := store.UpsertPage(db.Page{
		SourceID:  sourceID,
		URL:       "https://example.com/page",
		Title:     "Test Page",
		Content:   "This is the page content",
		Path:      []string{"Section A", "Test Page"},
	})
	if err != nil {
		t.Fatalf("UpsertPage: %v", err)
	}

	page, err := store.GetPageByURL("https://example.com/page")
	if err != nil {
		t.Fatalf("GetPageByURL: %v", err)
	}
	if page.Title != "Test Page" {
		t.Errorf("expected 'Test Page', got %q", page.Title)
	}
}
```

**Step 3: Run test to verify fails**

```bash
CGO_ENABLED=1 go test ./internal/db/... -v
```
Expected: FAIL

**Step 4: Implement db package**

```go
// internal/db/schema.go
package db

const schema = `
CREATE TABLE IF NOT EXISTS sources (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL,
    type          TEXT NOT NULL,
    url           TEXT,
    repo          TEXT,
    base_url      TEXT,
    space_key     TEXT,
    auth          TEXT,
    crawl_schedule TEXT,
    last_crawled  DATETIME,
    page_count    INTEGER DEFAULT 0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    url        TEXT NOT NULL UNIQUE,
    title      TEXT NOT NULL,
    content    TEXT NOT NULL,
    path       TEXT NOT NULL DEFAULT '[]',
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
    title, content, url UNINDEXED,
    content='pages',
    content_rowid='id'
);

CREATE TABLE IF NOT EXISTS tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER REFERENCES sources(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL,
    data       BLOB NOT NULL,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_id, provider)
);

CREATE TABLE IF NOT EXISTS crawl_jobs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'pending',
    started_at DATETIME,
    finished_at DATETIME,
    pages_crawled INTEGER DEFAULT 0,
    error      TEXT
);
`
```

```go
// internal/db/db.go
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct{ db *sql.DB }

type Source struct {
	ID            int64
	Name          string
	Type          string
	URL           string
	Repo          string
	BaseURL       string
	SpaceKey      string
	Auth          string
	CrawlSchedule string
	PageCount     int
}

type Page struct {
	ID       int64
	SourceID int64
	URL      string
	Title    string
	Content  string
	Path     []string
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite3", dsn+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) InsertSource(src Source) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO sources (name, type, url, repo, base_url, space_key, auth, crawl_schedule)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		src.Name, src.Type, src.URL, src.Repo, src.BaseURL, src.SpaceKey, src.Auth, src.CrawlSchedule,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListSources() ([]Source, error) {
	rows, err := s.db.Query(`SELECT id, name, type, url, repo, base_url, space_key, auth, crawl_schedule, page_count FROM sources`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []Source
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.Name, &src.Type, &src.URL, &src.Repo, &src.BaseURL, &src.SpaceKey, &src.Auth, &src.CrawlSchedule, &src.PageCount); err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *Store) UpsertPage(p Page) error {
	pathJSON, _ := json.Marshal(p.Path)
	_, err := s.db.Exec(
		`INSERT INTO pages (source_id, url, title, content, path)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(url) DO UPDATE SET
		   title=excluded.title, content=excluded.content,
		   path=excluded.path, updated_at=CURRENT_TIMESTAMP`,
		p.SourceID, p.URL, p.Title, p.Content, string(pathJSON),
	)
	if err != nil {
		return err
	}
	// Sync FTS index
	_, err = s.db.Exec(`INSERT INTO pages_fts(pages_fts) VALUES('rebuild')`)
	return err
}

func (s *Store) GetPageByURL(url string) (*Page, error) {
	var p Page
	var pathJSON string
	err := s.db.QueryRow(
		`SELECT id, source_id, url, title, content, path FROM pages WHERE url = ?`, url,
	).Scan(&p.ID, &p.SourceID, &p.URL, &p.Title, &p.Content, &pathJSON)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(pathJSON), &p.Path)
	return &p, nil
}
```

**Step 5: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/db/... -v
```
Expected: PASS

**Step 6: Commit**

```bash
git add internal/db/ go.mod go.sum
git commit -m "feat: add sqlite database layer with schema"
```

---

## Phase 2: Search Engine

### Task 5: Full-text search (FTS5)

**Files:**
- Create: `internal/search/fts.go`
- Create: `internal/search/fts_test.go`

**Step 1: Write failing test**

```go
// internal/search/fts_test.go
package search_test

import (
	"testing"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

func setupTestDB(t *testing.T) *db.Store {
	t.Helper()
	store, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestFTSSearch_ReturnsRelevantResults(t *testing.T) {
	store := setupTestDB(t)
	srcID, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "OAuth Setup", Content: "How to configure OAuth authentication"})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u2", Title: "Docker Guide", Content: "Running containers with Docker"})

	results, err := search.FTS(store, "OAuth authentication", 10)
	if err != nil {
		t.Fatalf("FTS: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}
	if results[0].URL != "u1" {
		t.Errorf("expected OAuth page first, got %s", results[0].URL)
	}
}
```

**Step 2: Run to verify fails**

```bash
CGO_ENABLED=1 go test ./internal/search/... -run TestFTS -v
```

**Step 3: Implement FTS search**

```go
// internal/search/fts.go
package search

import (
	"fmt"
	"github.com/documcp/documcp/internal/db"
)

type Result struct {
	URL      string
	Title    string
	Content  string
	SourceID int64
	Path     []string
	Score    float64
}

func FTS(store *db.Store, query string, limit int) ([]Result, error) {
	rows, err := store.DB().Query(`
		SELECT p.url, p.title, p.content, p.source_id, p.path,
		       bm25(pages_fts) as score
		FROM pages_fts
		JOIN pages p ON pages_fts.rowid = p.id
		WHERE pages_fts MATCH ?
		ORDER BY score
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	defer rows.Close()
	return scanResults(rows)
}
```

Note: Add `DB() *sql.DB` accessor method to `internal/db/db.go`:
```go
func (s *Store) DB() *sql.DB { return s.db }
```

Also add `scanResults` helper in a shared file `internal/search/scan.go`:
```go
// internal/search/scan.go
package search

import (
	"database/sql"
	"encoding/json"
)

func scanResults(rows *sql.Rows) ([]Result, error) {
	var results []Result
	for rows.Next() {
		var r Result
		var pathJSON string
		if err := rows.Scan(&r.URL, &r.Title, &r.Content, &r.SourceID, &pathJSON, &r.Score); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(pathJSON), &r.Path)
		results = append(results, r)
	}
	return results, rows.Err()
}
```

**Step 4: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/search/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/search/ internal/db/db.go
git commit -m "feat: add FTS5 full-text search"
```

---

### Task 6: ONNX embedding model wrapper

**Files:**
- Create: `internal/embed/embedder.go`
- Create: `internal/embed/embedder_test.go`

**Step 1: Add hugot dependency**

```bash
go get github.com/knights-analytics/hugot
```

**Step 2: Write failing test**

```go
// internal/embed/embedder_test.go
package embed_test

import (
	"os"
	"testing"
	"github.com/documcp/documcp/internal/embed"
)

func TestEmbedder_ProducesVectors(t *testing.T) {
	modelPath := os.Getenv("DOCUMCP_MODEL_PATH")
	if modelPath == "" {
		t.Skip("DOCUMCP_MODEL_PATH not set — skipping embedding test")
	}
	e, err := embed.New(modelPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer e.Close()

	vecs, err := e.Embed([]string{"hello world", "test sentence"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) == 0 {
		t.Error("expected non-empty vector")
	}
}
```

**Step 3: Implement embedder**

```go
// internal/embed/embedder.go
package embed

import (
	"fmt"
	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

type Embedder struct {
	session  *hugot.Session
	pipeline *pipelines.FeatureExtractionPipeline
}

func New(modelPath string) (*Embedder, error) {
	session, err := hugot.NewSession()
	if err != nil {
		return nil, fmt.Errorf("hugot session: %w", err)
	}
	pipeline, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
		ModelPath:    modelPath,
		Name:         "embedder",
		OnnxFilename: "model.onnx",
	})
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("embedding pipeline: %w", err)
	}
	return &Embedder{session: session, pipeline: pipeline}, nil
}

func (e *Embedder) Embed(texts []string) ([][]float32, error) {
	batch := &pipelines.FeatureExtractionBatch{}
	for _, t := range texts {
		batch.Input = append(batch.Input, pipelines.FeatureExtractionInput{Text: t})
	}
	output, err := e.pipeline.RunPipeline(batch)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	vecs := make([][]float32, len(output.Embeddings))
	for i, emb := range output.Embeddings {
		vecs[i] = emb
	}
	return vecs, nil
}

func (e *Embedder) Close() { e.session.Destroy() }
```

**Step 4: Run test (will skip without model, that's expected in CI)**

```bash
CGO_ENABLED=1 go test ./internal/embed/... -v
```
Expected: SKIP (no model path set) or PASS if model is present

**Step 5: Commit**

```bash
git add internal/embed/ go.mod go.sum
git commit -m "feat: add ONNX embedding wrapper"
```

---

### Task 7: sqlite-vec integration and vector search

**Files:**
- Modify: `internal/db/schema.go` (add vec table)
- Create: `internal/search/semantic.go`
- Modify: `internal/search/fts_test.go` (add vector search test)

**Step 1: Add sqlite-vec bindings**

```bash
go get github.com/asg017/sqlite-vec-go-bindings/cgo
```

**Step 2: Register sqlite-vec extension in db.go**

```go
// Add to internal/db/db.go imports and Open function:
import vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

func init() {
	vec.Auto() // registers the sqlite-vec extension automatically
}
```

**Step 3: Add vec table to schema**

```sql
-- Add to schema in internal/db/schema.go:
CREATE VIRTUAL TABLE IF NOT EXISTS page_embeddings USING vec0(
    page_id INTEGER PRIMARY KEY,
    embedding FLOAT[384]  -- all-MiniLM-L6-v2 dimension
);
```

**Step 4: Add UpsertEmbedding to store**

```go
// Add to internal/db/db.go:
func (s *Store) UpsertEmbedding(pageID int64, vec []float32) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO page_embeddings(page_id, embedding) VALUES (?, ?)`,
		pageID, sqliteVecBlob(vec),
	)
	return err
}

func sqliteVecBlob(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(b[i*4:], bits)
	}
	return b
}
```

**Step 5: Implement semantic search**

```go
// internal/search/semantic.go
package search

import (
	"fmt"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
)

func Semantic(store *db.Store, embedder *embed.Embedder, query string, limit int) ([]Result, error) {
	vecs, err := embedder.Embed([]string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	blob := db.Float32ToBlob(vecs[0])
	rows, err := store.DB().Query(`
		SELECT p.url, p.title, p.content, p.source_id, p.path,
		       pe.distance as score
		FROM page_embeddings pe
		JOIN pages p ON pe.page_id = p.id
		WHERE pe.embedding MATCH ?
		  AND k = ?
		ORDER BY pe.distance
	`, blob, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic query: %w", err)
	}
	defer rows.Close()
	return scanResults(rows)
}
```

**Step 6: Implement RRF (reciprocal rank fusion) merger**

```go
// internal/search/rrf.go
package search

// MergeRRF combines FTS and semantic results using reciprocal rank fusion.
// k=60 is the standard RRF constant.
func MergeRRF(ftsResults, semanticResults []Result, limit int) []Result {
	const k = 60.0
	scores := map[string]float64{}
	byURL := map[string]Result{}

	for i, r := range ftsResults {
		scores[r.URL] += 1.0 / (k + float64(i+1))
		byURL[r.URL] = r
	}
	for i, r := range semanticResults {
		scores[r.URL] += 1.0 / (k + float64(i+1))
		byURL[r.URL] = r
	}

	type scored struct {
		url   string
		score float64
	}
	var ranked []scored
	for url, score := range scores {
		ranked = append(ranked, scored{url, score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	results := make([]Result, 0, limit)
	for _, s := range ranked {
		if len(results) >= limit {
			break
		}
		r := byURL[s.url]
		r.Score = s.score
		results = append(results, r)
	}
	return results
}
```

**Step 7: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/... -v
```
Expected: PASS (semantic tests skip without model)

**Step 8: Commit**

```bash
git add internal/ go.mod go.sum
git commit -m "feat: add sqlite-vec, semantic search, and RRF merging"
```

---

### Task 8: Hierarchical browse (TOC)

**Files:**
- Create: `internal/search/browse.go`
- Create: `internal/search/browse_test.go`

**Step 1: Write failing test**

```go
// internal/search/browse_test.go
func TestBrowseSource_TopLevel(t *testing.T) {
	store := setupTestDB(t)
	srcID, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "Auth Overview", Path: []string{"Authentication", "Auth Overview"}})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u2", Title: "OAuth Setup", Path: []string{"Authentication", "OAuth Setup"}})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u3", Title: "Docker Guide", Path: []string{"Deployment", "Docker Guide"}})

	sections, err := search.BrowseTopLevel(store, srcID)
	if err != nil {
		t.Fatalf("BrowseTopLevel: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
}

func TestBrowseSource_Section(t *testing.T) {
	store := setupTestDB(t)
	srcID, _ := store.InsertSource(db.Source{Name: "S", Type: "web"})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u1", Title: "Auth Overview", Path: []string{"Authentication", "Auth Overview"}})
	store.UpsertPage(db.Page{SourceID: srcID, URL: "u2", Title: "OAuth Setup", Path: []string{"Authentication", "OAuth Setup"}})

	pages, err := search.BrowseSection(store, srcID, "Authentication")
	if err != nil {
		t.Fatalf("BrowseSection: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
}
```

**Step 2: Implement browse**

```go
// internal/search/browse.go
package search

import (
	"encoding/json"
	"fmt"
	"github.com/documcp/documcp/internal/db"
)

type Section struct {
	Name      string
	PageCount int
}

type PageRef struct {
	URL   string
	Title string
	Path  []string
}

func BrowseTopLevel(store *db.Store, sourceID int64) ([]Section, error) {
	rows, err := store.DB().Query(
		`SELECT path FROM pages WHERE source_id = ?`, sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("browse top level: %w", err)
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var pathJSON string
		if err := rows.Scan(&pathJSON); err != nil {
			return nil, err
		}
		var path []string
		json.Unmarshal([]byte(pathJSON), &path)
		if len(path) > 0 {
			counts[path[0]]++
		}
	}
	var sections []Section
	for name, count := range counts {
		sections = append(sections, Section{Name: name, PageCount: count})
	}
	return sections, nil
}

func BrowseSection(store *db.Store, sourceID int64, section string) ([]PageRef, error) {
	rows, err := store.DB().Query(
		`SELECT url, title, path FROM pages WHERE source_id = ?`, sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("browse section: %w", err)
	}
	defer rows.Close()

	var pages []PageRef
	for rows.Next() {
		var ref PageRef
		var pathJSON string
		if err := rows.Scan(&ref.URL, &ref.Title, &pathJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(pathJSON), &ref.Path)
		if len(ref.Path) > 0 && ref.Path[0] == section {
			pages = append(pages, ref)
		}
	}
	return pages, nil
}
```

**Step 3: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/search/... -v
```
Expected: PASS

**Step 4: Commit**

```bash
git add internal/search/
git commit -m "feat: add hierarchical browse (top-level sections and section pages)"
```

---

## Phase 3: Source Adapters

### Task 9: Adapter interface and Page types

**Files:**
- Create: `internal/adapter/adapter.go`

**Step 1: Define the interface**

```go
// internal/adapter/adapter.go
package adapter

import (
	"context"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

// Adapter is implemented by each documentation source type.
type Adapter interface {
	// Crawl fetches all pages from the source, sending them to the returned channel.
	// The channel is closed when crawling is complete or ctx is cancelled.
	Crawl(ctx context.Context, source config.SourceConfig) (<-chan db.Page, error)
	// NeedsAuth returns true if the source requires authentication.
	NeedsAuth(source config.SourceConfig) bool
}

// Registry maps source type strings to their adapter implementations.
var Registry = map[string]Adapter{}

func Register(sourceType string, a Adapter) {
	Registry[sourceType] = a
}
```

**Step 2: Commit**

```bash
git add internal/adapter/
git commit -m "feat: add adapter interface and registry"
```

---

### Task 10: Generic web adapter — sitemap parsing

**Files:**
- Create: `internal/adapter/web/web.go`
- Create: `internal/adapter/web/sitemap.go`
- Create: `internal/adapter/web/sitemap_test.go`

**Step 1: Add HTML parsing dependency**

```bash
go get golang.org/x/net/html
```

**Step 2: Write failing test for sitemap parsing**

```go
// internal/adapter/web/sitemap_test.go
package web_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/documcp/documcp/internal/adapter/web"
)

func TestParseSitemap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<?xml version="1.0"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/page1</loc></url>
  <url><loc>https://example.com/page2</loc></url>
</urlset>`))
	}))
	defer srv.Close()

	urls, err := web.ParseSitemap(srv.URL + "/sitemap.xml", nil)
	if err != nil {
		t.Fatalf("ParseSitemap: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(urls))
	}
}
```

**Step 3: Implement sitemap parser**

```go
// internal/adapter/web/sitemap.go
package web

import (
	"encoding/xml"
	"fmt"
	"net/http"
)

type urlSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

func ParseSitemap(sitemapURL string, client *http.Client) ([]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(sitemapURL)
	if err != nil {
		return nil, fmt.Errorf("fetch sitemap: %w", err)
	}
	defer resp.Body.Close()
	var us urlSet
	if err := xml.NewDecoder(resp.Body).Decode(&us); err != nil {
		return nil, fmt.Errorf("parse sitemap: %w", err)
	}
	urls := make([]string, len(us.URLs))
	for i, u := range us.URLs {
		urls[i] = u.Loc
	}
	return urls, nil
}
```

**Step 4: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/adapter/web/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/adapter/web/ go.mod go.sum
git commit -m "feat: add web adapter sitemap parser"
```

---

### Task 11: Generic web adapter — HTML text extraction and crawling

**Files:**
- Create: `internal/adapter/web/extract.go`
- Create: `internal/adapter/web/extract_test.go`
- Modify: `internal/adapter/web/web.go`

**Step 1: Write failing test for extraction**

```go
// internal/adapter/web/extract_test.go
func TestExtractText_RemovesNavAndScript(t *testing.T) {
	html := `<html><body>
		<nav>Navigation</nav>
		<main><h1>Title</h1><p>Main content here.</p></main>
		<script>alert("x")</script>
	</body></html>`

	title, content := web.ExtractText(strings.NewReader(html))
	if title != "Title" {
		t.Errorf("expected 'Title', got %q", title)
	}
	if strings.Contains(content, "Navigation") {
		t.Error("nav content should be excluded")
	}
	if strings.Contains(content, "alert") {
		t.Error("script content should be excluded")
	}
	if !strings.Contains(content, "Main content") {
		t.Error("main content should be included")
	}
}
```

**Step 2: Implement HTML extraction**

```go
// internal/adapter/web/extract.go
package web

import (
	"io"
	"strings"
	"golang.org/x/net/html"
)

var skipTags = map[string]bool{
	"script": true, "style": true, "nav": true,
	"footer": true, "header": true, "aside": true,
}

func ExtractText(r io.Reader) (title, content string) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if skipTags[tag] {
				return
			}
			if tag == "h1" && title == "" {
				title = nodeText(n)
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title, strings.TrimSpace(sb.String())
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return strings.TrimSpace(sb.String())
}
```

**Step 3: Implement web adapter Crawl**

```go
// internal/adapter/web/web.go
package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"github.com/documcp/documcp/internal/adapter"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

func init() {
	adapter.Register("web", &WebAdapter{})
}

type WebAdapter struct{}

func (a *WebAdapter) NeedsAuth(src config.SourceConfig) bool {
	return src.Auth != ""
}

func (a *WebAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (<-chan db.Page, error) {
	ch := make(chan db.Page, 10)
	client := http.DefaultClient

	go func() {
		defer close(ch)
		sitemapURL := src.URL + "/sitemap.xml"
		urls, err := ParseSitemap(sitemapURL, client)
		if err != nil || len(urls) == 0 {
			// Fallback: just crawl the root URL
			urls = []string{src.URL}
		}

		base, _ := url.Parse(src.URL)
		visited := map[string]bool{}

		for _, pageURL := range urls {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if visited[pageURL] {
				continue
			}
			visited[pageURL] = true

			page, err := fetchPage(client, pageURL, sourceID, base)
			if err != nil {
				continue
			}
			ch <- page
		}
	}()
	return ch, nil
}

func fetchPage(client *http.Client, pageURL string, sourceID int64, base *url.URL) (db.Page, error) {
	resp, err := client.Get(pageURL)
	if err != nil {
		return db.Page{}, fmt.Errorf("fetch %s: %w", pageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return db.Page{}, fmt.Errorf("non-200 from %s: %d", pageURL, resp.StatusCode)
	}
	title, content := ExtractText(resp.Body)
	if title == "" {
		title = pageURL
	}
	u, _ := url.Parse(pageURL)
	path := urlToPath(u, base)
	return db.Page{
		SourceID: sourceID,
		URL:      pageURL,
		Title:    title,
		Content:  content,
		Path:     path,
	}, nil
}

func urlToPath(u, base *url.URL) []string {
	rel, _ := url.Parse(u.Path)
	parts := strings.Split(strings.Trim(rel.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		return []string{"Home"}
	}
	return parts
}
```

**Step 4: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/adapter/web/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/adapter/web/
git commit -m "feat: add generic web adapter with sitemap crawling and HTML extraction"
```

---

### Task 12: GitHub Wiki adapter

**Files:**
- Create: `internal/adapter/github/github.go`
- Create: `internal/adapter/github/github_test.go`

**Step 1: Write test with mock HTTP**

```go
// internal/adapter/github/github_test.go
func TestGitHubAdapter_BuildsHierarchy(t *testing.T) {
	// Mock GitHub API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"title": "Home", "path": "Home.md", "sha": "abc"},
			{"title": "Authentication Guide", "path": "Authentication-Guide.md", "sha": "def"},
		})
	}))
	defer srv.Close()

	a := github.NewAdapter(srv.URL) // injectable base URL for testing
	// Verify it returns pages
}
```

**Step 2: Implement GitHub Wiki adapter**

```go
// internal/adapter/github/github.go
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"github.com/documcp/documcp/internal/adapter"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

func init() {
	adapter.Register("github_wiki", &GitHubAdapter{baseURL: "https://api.github.com"})
}

type GitHubAdapter struct{ baseURL string }

func NewAdapter(baseURL string) *GitHubAdapter { return &GitHubAdapter{baseURL: baseURL} }

func (a *GitHubAdapter) NeedsAuth(src config.SourceConfig) bool { return true }

func (a *GitHubAdapter) Crawl(ctx context.Context, src config.SourceConfig, sourceID int64) (<-chan db.Page, error) {
	ch := make(chan db.Page, 10)
	go func() {
		defer close(ch)
		token, err := loadGitHubToken()
		if err != nil {
			return
		}
		client := &http.Client{}
		pages, err := fetchWikiPages(client, a.baseURL, src.Repo, token)
		if err != nil {
			return
		}
		for _, p := range pages {
			p.SourceID = sourceID
			select {
			case ch <- p:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

type wikiPage struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func fetchWikiPages(client *http.Client, baseURL, repo, token string) ([]db.Page, error) {
	url := fmt.Sprintf("%s/repos/%s/git/trees/master?recursive=1", baseURL, repo)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Parse tree, fetch .md files, build Page objects
	// (simplified — real implementation fetches each blob content)
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			URL  string `json:"url"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	json.NewDecoder(resp.Body).Decode(&tree)

	var pages []db.Page
	for _, item := range tree.Tree {
		if item.Type != "blob" || !strings.HasSuffix(item.Path, ".md") {
			continue
		}
		title := strings.TrimSuffix(item.Path, ".md")
		title = strings.ReplaceAll(title, "-", " ")
		pages = append(pages, db.Page{
			URL:   fmt.Sprintf("https://github.com/%s/wiki/%s", repo, item.Path),
			Title: title,
			Path:  []string{"Wiki", title},
		})
	}
	return pages, nil
}

func loadGitHubToken() (string, error) {
	// Try ~/.config/gh/hosts.yml first, then stored token from DB
	// (implementation in auth package)
	return "", nil // placeholder
}
```

**Step 3: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/adapter/github/... -v
```

**Step 4: Commit**

```bash
git add internal/adapter/github/
git commit -m "feat: add GitHub Wiki adapter"
```

---

### Task 13: Azure DevOps Wiki adapter

**Files:**
- Create: `internal/adapter/azuredevops/azuredevops.go`

Follows the same pattern as the GitHub adapter. Calls the Azure DevOps REST API (`dev.azure.com/{org}/{project}/_apis/wiki/wikis/{wikiId}/pages`) with a Bearer token from the Microsoft device code flow. Register as `"azure_devops"` source type.

**Step 1: Implement and register adapter (same structure as Task 12)**

**Step 2: Commit**

```bash
git add internal/adapter/azuredevops/
git commit -m "feat: add Azure DevOps Wiki adapter"
```

---

## Phase 4: Authentication

### Task 14: Microsoft device code flow

**Files:**
- Create: `internal/auth/microsoft.go`
- Create: `internal/auth/microsoft_test.go`

**Step 1: Write test with mock OAuth server**

```go
// internal/auth/microsoft_test.go
func TestMicrosoftDeviceCodeFlow_InitiatesSuccessfully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "test-device-code",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://microsoft.com/devicelogin",
			"expires_in":       900,
			"interval":         5,
		})
	}))
	defer srv.Close()

	flow, err := auth.NewMicrosoftDeviceFlow(srv.URL, "consumers")
	if err != nil {
		t.Fatalf("NewMicrosoftDeviceFlow: %v", err)
	}
	if flow.UserCode == "" {
		t.Error("expected UserCode to be populated")
	}
	if flow.VerificationURI == "" {
		t.Error("expected VerificationURI to be populated")
	}
}
```

**Step 2: Implement device code flow**

```go
// internal/auth/microsoft.go
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AzureCLIClientID is the well-known public client ID used by Azure CLI.
// No app registration required — works across all tenants without admin consent.
const AzureCLIClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"

type MicrosoftDeviceFlow struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        int
	tokenEndpoint   string
}

func NewMicrosoftDeviceFlow(baseURL, tenant string) (*MicrosoftDeviceFlow, error) {
	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/devicecode", baseURL, tenant)
	resp, err := http.PostForm(endpoint, url.Values{
		"client_id": {AzureCLIClientID},
		"scope":     {"https://management.azure.com/user_impersonation offline_access"},
	})
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &MicrosoftDeviceFlow{
		DeviceCode:      result.DeviceCode,
		UserCode:        result.UserCode,
		VerificationURI: result.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
		Interval:        result.Interval,
		tokenEndpoint:   strings.Replace(endpoint, "devicecode", "token", 1),
	}, nil
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (f *MicrosoftDeviceFlow) Poll(ctx context.Context) (*Token, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(f.Interval) * time.Second):
		}
		resp, err := http.PostForm(f.tokenEndpoint, url.Values{
			"client_id":   {AzureCLIClientID},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {f.DeviceCode},
		})
		if err != nil {
			continue
		}
		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			Error        string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Error == "authorization_pending" {
			continue
		}
		if result.AccessToken != "" {
			return &Token{
				AccessToken:  result.AccessToken,
				RefreshToken: result.RefreshToken,
				ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
			}, nil
		}
		return nil, fmt.Errorf("auth error: %s", result.Error)
	}
}
```

**Step 3: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/auth/... -v
```
Expected: PASS

**Step 4: Commit**

```bash
git add internal/auth/
git commit -m "feat: add Microsoft device code flow auth"
```

---

### Task 15: GitHub device code flow

**Files:**
- Create: `internal/auth/github.go`

Same structure as Microsoft device code flow but using:
- Device code endpoint: `https://github.com/login/device/code`
- Token endpoint: `https://github.com/login/oauth/access_token`
- Scope: `repo` (for private wiki access)
- Also reads from `~/.config/gh/hosts.yml` as a first-try fallback

**Step 1: Implement, write test, commit**

```bash
git commit -m "feat: add GitHub device code flow auth"
```

---

### Task 16: Token storage (encrypted in SQLite)

**Files:**
- Create: `internal/auth/store.go`
- Create: `internal/auth/store_test.go`

**Step 1: Write failing test**

```go
func TestTokenStore_SaveAndLoad(t *testing.T) {
	dbStore, _ := db.Open(":memory:")
	defer dbStore.Close()

	ts := auth.NewTokenStore(dbStore, []byte("test-encryption-key-32-bytes!!!!"))
	token := &auth.Token{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	err := ts.Save(1, "microsoft", token)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := ts.Load(1, "microsoft")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AccessToken != token.AccessToken {
		t.Errorf("expected %q, got %q", token.AccessToken, loaded.AccessToken)
	}
}
```

**Step 2: Implement with AES-GCM encryption**

```go
// internal/auth/store.go
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"github.com/documcp/documcp/internal/db"
)

type TokenStore struct {
	db  *db.Store
	key []byte // 32-byte AES-256 key
}

func NewTokenStore(store *db.Store, key []byte) *TokenStore {
	return &TokenStore{db: store, key: key}
}

func (ts *TokenStore) Save(sourceID int64, provider string, token *Token) error {
	plaintext, _ := json.Marshal(token)
	ciphertext, err := ts.encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}
	return ts.db.UpsertToken(sourceID, provider, ciphertext, token.ExpiresAt)
}

func (ts *TokenStore) Load(sourceID int64, provider string) (*Token, error) {
	ciphertext, err := ts.db.GetToken(sourceID, provider)
	if err != nil {
		return nil, err
	}
	plaintext, err := ts.decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}
	var token Token
	json.Unmarshal(plaintext, &token)
	return &token, nil
}

func (ts *TokenStore) encrypt(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(ts.key)
	if err != nil {
		return nil, err
	}
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (ts *TokenStore) decrypt(data []byte) ([]byte, error) {
	block, _ := aes.NewCipher(ts.key)
	gcm, _ := cipher.NewGCM(block)
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
```

**Step 3: Add UpsertToken/GetToken to db package**

Add corresponding methods to `internal/db/db.go` that read/write the `tokens` table.

**Step 4: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/auth/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/ internal/db/
git commit -m "feat: add encrypted token storage"
```

---

## Phase 5: Crawler & Scheduler

### Task 17: Crawl orchestrator

**Files:**
- Create: `internal/crawler/crawler.go`
- Create: `internal/crawler/crawler_test.go`

**Step 1: Write failing test**

```go
func TestCrawler_IndexesPages(t *testing.T) {
	store, _ := db.Open(":memory:")
	defer store.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			w.Write([]byte(`<?xml version="1.0"?><urlset><url><loc>` + srv.URL + `/page1</loc></url></urlset>`))
			return
		}
		w.Write([]byte(`<html><body><h1>Test Page</h1><p>Content here.</p></body></html>`))
	}))
	defer srv.Close()

	srcID, _ := store.InsertSource(db.Source{Name: "Test", Type: "web", URL: srv.URL})
	c := crawler.New(store, nil) // nil embedder = skip embeddings in test
	err := c.Crawl(context.Background(), db.Source{ID: srcID, Type: "web", URL: srv.URL})
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	sources, _ := store.ListSources()
	if sources[0].PageCount == 0 {
		t.Error("expected pages to be indexed")
	}
}
```

**Step 2: Implement crawler**

```go
// internal/crawler/crawler.go
package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"github.com/documcp/documcp/internal/adapter"
	_ "github.com/documcp/documcp/internal/adapter/github"
	_ "github.com/documcp/documcp/internal/adapter/web"
	_ "github.com/documcp/documcp/internal/adapter/azuredevops"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
)

type Crawler struct {
	store    *db.Store
	embedder *embed.Embedder // nil = skip embeddings
}

func New(store *db.Store, embedder *embed.Embedder) *Crawler {
	return &Crawler{store: store, embedder: embedder}
}

func (c *Crawler) Crawl(ctx context.Context, src db.Source) error {
	a, ok := adapter.Registry[src.Type]
	if !ok {
		return fmt.Errorf("unknown source type: %s", src.Type)
	}

	cfgSrc := sourceToConfig(src)
	pages, err := a.Crawl(ctx, cfgSrc, src.ID)
	if err != nil {
		return fmt.Errorf("crawl: %w", err)
	}

	count := 0
	for page := range pages {
		if err := c.store.UpsertPage(page); err != nil {
			slog.Error("upsert page", "url", page.URL, "err", err)
			continue
		}
		if c.embedder != nil {
			if err := c.indexEmbedding(ctx, page); err != nil {
				slog.Error("embed page", "url", page.URL, "err", err)
			}
		}
		count++
	}
	return c.store.UpdateSourcePageCount(src.ID, count)
}

func (c *Crawler) indexEmbedding(ctx context.Context, page db.Page) error {
	p, err := c.store.GetPageByURL(page.URL)
	if err != nil {
		return err
	}
	vecs, err := c.embedder.Embed([]string{page.Title + " " + page.Content})
	if err != nil {
		return err
	}
	return c.store.UpsertEmbedding(p.ID, vecs[0])
}
```

**Step 3: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/crawler/... -v
```
Expected: PASS

**Step 4: Commit**

```bash
git add internal/crawler/
git commit -m "feat: add crawl orchestrator"
```

---

### Task 18: Cron scheduler

**Files:**
- Create: `internal/crawler/scheduler.go`

**Step 1: Add cron dependency**

```bash
go get github.com/robfig/cron/v3
```

**Step 2: Implement scheduler**

```go
// internal/crawler/scheduler.go
package crawler

import (
	"context"
	"log/slog"
	"github.com/robfig/cron/v3"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
)

type Scheduler struct {
	cron    *cron.Cron
	crawler *Crawler
	store   *db.Store
}

func NewScheduler(c *Crawler, store *db.Store) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		crawler: c,
		store:   store,
	}
}

func (s *Scheduler) Load(cfg *config.Config) {
	s.cron.Stop()
	s.cron = cron.New()
	for _, src := range cfg.Sources {
		if src.CrawlSchedule == "" {
			continue
		}
		srcCopy := src
		s.cron.AddFunc(src.CrawlSchedule, func() {
			sources, _ := s.store.ListSources()
			for _, dbSrc := range sources {
				if dbSrc.Name == srcCopy.Name {
					slog.Info("scheduled crawl", "source", dbSrc.Name)
					s.crawler.Crawl(context.Background(), dbSrc)
					return
				}
			}
		})
	}
	s.cron.Start()
}

func (s *Scheduler) Stop() { s.cron.Stop() }
```

**Step 3: Commit**

```bash
git add internal/crawler/ go.mod go.sum
git commit -m "feat: add cron-based crawl scheduler"
```

---

## Phase 6: MCP Server

### Task 19: MCP server setup with go-sdk

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/server_test.go`

**Step 1: Add MCP SDK**

```bash
go get github.com/modelcontextprotocol/go-sdk@latest
```

**Step 2: Write failing test**

```go
// internal/mcp/server_test.go
func TestMCPServer_ListSourcesTool(t *testing.T) {
	store, _ := db.Open(":memory:")
	store.InsertSource(db.Source{Name: "Test Docs", Type: "web"})

	s := mcp.NewServer(store, nil)
	// Invoke list_sources tool directly
	result, err := s.InvokeTool(context.Background(), "list_sources", nil)
	if err != nil {
		t.Fatalf("list_sources: %v", err)
	}
	if !strings.Contains(result, "Test Docs") {
		t.Errorf("expected 'Test Docs' in result, got: %s", result)
	}
}
```

**Step 3: Implement MCP server and tools**

```go
// internal/mcp/server.go
package mcp

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
	"github.com/documcp/documcp/internal/search"
)

type Server struct {
	store    *db.Store
	embedder *embed.Embedder
	mcp      *mcp.Server
}

func NewServer(store *db.Store, embedder *embed.Embedder) *Server {
	s := &Server{store: store, embedder: embedder}
	s.mcp = mcp.NewServer(&mcp.ServerConfig{
		Name:    "DocuMcp",
		Version: "1.0.0",
	})
	s.registerTools()
	return s
}

func (s *Server) Handler() http.Handler { return s.mcp.HTTPHandler() }
```

```go
// internal/mcp/tools.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/documcp/documcp/internal/search"
)

func (s *Server) registerTools() {
	s.mcp.AddTool(mcp.Tool{
		Name:        "list_sources",
		Description: "List all configured documentation sources and their crawl status",
	}, s.listSources)

	s.mcp.AddTool(mcp.Tool{
		Name:        "search_docs",
		Description: "Search indexed documentation using hybrid full-text and semantic search",
		InputSchema: mcp.ObjectSchema{
			Properties: map[string]mcp.Schema{
				"query":  {Type: "string", Description: "Search query"},
				"source": {Type: "string", Description: "Optional: limit to a specific source name"},
			},
			Required: []string{"query"},
		},
	}, s.searchDocs)

	s.mcp.AddTool(mcp.Tool{
		Name:        "browse_source",
		Description: "Browse documentation hierarchy. Without section: returns top-level sections. With section: returns pages within that section.",
		InputSchema: mcp.ObjectSchema{
			Properties: map[string]mcp.Schema{
				"source":  {Type: "string", Description: "Source name"},
				"section": {Type: "string", Description: "Optional: section name to drill into"},
			},
			Required: []string{"source"},
		},
	}, s.browseSource)

	s.mcp.AddTool(mcp.Tool{
		Name:        "get_page",
		Description: "Retrieve the full content of a specific documentation page by URL",
		InputSchema: mcp.ObjectSchema{
			Properties: map[string]mcp.Schema{
				"url": {Type: "string", Description: "Page URL"},
			},
			Required: []string{"url"},
		},
	}, s.getPage)
}

func (s *Server) listSources(ctx context.Context, args map[string]any) (string, error) {
	sources, err := s.store.ListSources()
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(sources, "", "  ")
	return string(out), nil
}

func (s *Server) searchDocs(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	ftsResults, _ := search.FTS(s.store, query, 20)
	var results []search.Result
	if s.embedder != nil {
		semResults, _ := search.Semantic(s.store, s.embedder, query, 20)
		results = search.MergeRRF(ftsResults, semResults, 10)
	} else {
		results = ftsResults
		if len(results) > 10 {
			results = results[:10]
		}
	}
	out, _ := json.MarshalIndent(results, "", "  ")
	return string(out), nil
}

func (s *Server) browseSource(ctx context.Context, args map[string]any) (string, error) {
	sourceName, _ := args["source"].(string)
	section, _ := args["section"].(string)

	srcID, err := s.store.GetSourceIDByName(sourceName)
	if err != nil {
		return "", fmt.Errorf("source not found: %s", sourceName)
	}

	if section == "" {
		sections, err := search.BrowseTopLevel(s.store, srcID)
		if err != nil {
			return "", err
		}
		out, _ := json.MarshalIndent(sections, "", "  ")
		return string(out), nil
	}

	pages, err := search.BrowseSection(s.store, srcID, section)
	if err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(pages, "", "  ")
	return string(out), nil
}

func (s *Server) getPage(ctx context.Context, args map[string]any) (string, error) {
	pageURL, _ := args["url"].(string)
	page, err := s.store.GetPageByURL(pageURL)
	if err != nil {
		return "", fmt.Errorf("page not found: %s", pageURL)
	}
	return page.Content, nil
}
```

**Step 4: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/mcp/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/mcp/ go.mod go.sum
git commit -m "feat: add MCP server with search, browse, and get_page tools"
```

---

## Phase 7: REST API (for Web UI)

### Task 20: REST API server and source endpoints

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/handlers.go`
- Create: `internal/api/handlers_test.go`

**Step 1: Write failing tests**

```go
// internal/api/handlers_test.go
func TestGetSources(t *testing.T) {
	store, _ := db.Open(":memory:")
	store.InsertSource(db.Source{Name: "Docs", Type: "web"})

	srv := api.NewServer(store, nil, nil)
	r := httptest.NewRequest("GET", "/api/sources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var sources []db.Source
	json.NewDecoder(w.Body).Decode(&sources)
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
}

func TestPostCrawl_TriggersCrawl(t *testing.T) {
	// Test that POST /api/sources/{id}/crawl kicks off a crawl job
}
```

**Step 2: Implement REST API**

```go
// internal/api/server.go
package api

import (
	"net/http"
	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
)

type Server struct {
	store    *db.Store
	crawler  *crawler.Crawler
	mux      *http.ServeMux
}

func NewServer(store *db.Store, c *crawler.Crawler, mcpHandler http.Handler) *Server {
	s := &Server{store: store, crawler: c, mux: http.NewServeMux()}
	s.routes(mcpHandler)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes(mcpHandler http.Handler) {
	s.mux.HandleFunc("GET /api/sources", s.listSources)
	s.mux.HandleFunc("POST /api/sources", s.createSource)
	s.mux.HandleFunc("DELETE /api/sources/{id}", s.deleteSource)
	s.mux.HandleFunc("POST /api/sources/{id}/crawl", s.triggerCrawl)
	s.mux.HandleFunc("GET /api/search", s.search)
	s.mux.Handle("/mcp", mcpHandler)
	s.mux.Handle("/", http.FileServer(webFS())) // embedded static files
}
```

**Step 3: Implement handlers**

```go
// internal/api/handlers.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListSources()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources)
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request) {
	var src db.Source
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id, err := s.store.InsertSource(src)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	src.ID = id
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(src)
}

func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.store.DeleteSource(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) triggerCrawl(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	src, err := s.store.GetSource(id)
	if err != nil {
		http.Error(w, "source not found", 404)
		return
	}
	go s.crawler.Crawl(context.Background(), *src)
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{"status": "crawl started"})
}
```

**Step 4: Run tests**

```bash
CGO_ENABLED=1 go test ./internal/api/... -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat: add REST API for web UI (sources, crawl trigger, search)"
```

---

### Task 21: Auth flow REST endpoints

**Files:**
- Modify: `internal/api/handlers.go`

Add endpoints:
- `POST /api/sources/{id}/auth/start` — initiates device code flow, returns `{user_code, verification_uri}`
- `GET /api/sources/{id}/auth/poll` — polls for token completion (SSE or polling)
- `DELETE /api/sources/{id}/auth` — disconnects (deletes stored token)

These endpoints are called by the Web UI when the user clicks "Connect" on a source. The device code and verification URL are displayed inline in the UI.

**Commit:**

```bash
git commit -m "feat: add auth flow REST endpoints (device code initiation and polling)"
```

---

## Phase 8: Web UI

### Task 22: Base layout and dark theme

**Files:**
- Create: `web/static/index.html`
- Create: `web/static/style.css`
- Create: `web/embed.go`

**Step 1: Create embed.go**

```go
// web/embed.go
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

func FileSystem() http.FileSystem {
	sub, _ := fs.Sub(staticFiles, "static")
	return http.FS(sub)
}
```

**Step 2: Create dark-themed base layout**

```html
<!-- web/static/index.html -->
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>DocuMcp</title>
  <link rel="stylesheet" href="/style.css">
  <script defer src="https://unpkg.com/alpinejs@3/dist/cdn.min.js"></script>
  <script defer src="/app.js"></script>
</head>
<body x-data="app()" x-init="init()">
  <nav class="sidebar">
    <div class="logo">DocuMcp</div>
    <a href="#" @click.prevent="view='sources'" :class="{active: view==='sources'}">Sources</a>
    <a href="#" @click.prevent="view='search'" :class="{active: view==='search'}">Search</a>
  </nav>
  <main class="content">
    <div x-show="view==='sources'" x-cloak>
      <!-- Sources dashboard -->
    </div>
    <div x-show="view==='search'" x-cloak>
      <!-- Search -->
    </div>
  </main>
</body>
</html>
```

```css
/* web/static/style.css — dark theme */
:root {
  --bg:        #0f1117;
  --surface:   #1a1d27;
  --border:    #2a2d3a;
  --text:      #e2e8f0;
  --muted:     #8892a4;
  --accent:    #6366f1;
  --accent-hover: #818cf8;
  --success:   #22c55e;
  --danger:    #ef4444;
  --warning:   #f59e0b;
}

* { box-sizing: border-box; margin: 0; padding: 0; }
body { background: var(--bg); color: var(--text); font-family: 'Inter', system-ui, sans-serif; display: flex; min-height: 100vh; }

.sidebar { width: 220px; background: var(--surface); border-right: 1px solid var(--border); padding: 1.5rem 1rem; display: flex; flex-direction: column; gap: 0.5rem; }
.logo { font-size: 1.2rem; font-weight: 700; color: var(--accent); padding: 0.5rem; margin-bottom: 1rem; }
.sidebar a { color: var(--muted); text-decoration: none; padding: 0.6rem 0.75rem; border-radius: 6px; transition: all 0.15s; }
.sidebar a:hover, .sidebar a.active { background: var(--border); color: var(--text); }

.content { flex: 1; padding: 2rem; overflow-y: auto; }
.card { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 1.25rem; margin-bottom: 1rem; }
.badge { display: inline-block; padding: 0.2rem 0.6rem; border-radius: 4px; font-size: 0.75rem; font-weight: 600; }
.badge-ok { background: rgba(34,197,94,0.15); color: var(--success); }
.badge-pending { background: rgba(245,158,11,0.15); color: var(--warning); }
.badge-error { background: rgba(239,68,68,0.15); color: var(--danger); }
button { background: var(--accent); color: white; border: none; padding: 0.5rem 1rem; border-radius: 6px; cursor: pointer; font-size: 0.875rem; }
button:hover { background: var(--accent-hover); }
button.secondary { background: var(--border); color: var(--text); }
input, select { background: var(--bg); border: 1px solid var(--border); color: var(--text); padding: 0.5rem 0.75rem; border-radius: 6px; width: 100%; margin-bottom: 0.75rem; }
input:focus, select:focus { outline: 2px solid var(--accent); border-color: transparent; }
[x-cloak] { display: none; }
```

**Step 3: Commit**

```bash
git add web/
git commit -m "feat: add web UI base layout with dark theme"
```

---

### Task 23: Sources dashboard

**Files:**
- Modify: `web/static/index.html`
- Create: `web/static/app.js`

**Step 1: Implement sources dashboard with Alpine.js**

```js
// web/static/app.js
function app() {
  return {
    view: 'sources',
    sources: [],
    showAddForm: false,
    newSource: { name: '', type: 'web', url: '', repo: '', crawl_schedule: '' },
    deviceCodePending: null, // {user_code, verification_uri}

    async init() {
      await this.loadSources()
    },

    async loadSources() {
      const r = await fetch('/api/sources')
      this.sources = await r.json()
    },

    async addSource() {
      await fetch('/api/sources', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(this.newSource)
      })
      this.showAddForm = false
      this.newSource = { name: '', type: 'web', url: '', repo: '', crawl_schedule: '' }
      await this.loadSources()
    },

    async crawlNow(id) {
      await fetch(`/api/sources/${id}/crawl`, { method: 'POST' })
      // Poll for status update
      setTimeout(() => this.loadSources(), 2000)
    },

    async connectAuth(id) {
      const r = await fetch(`/api/sources/${id}/auth/start`, { method: 'POST' })
      this.deviceCodePending = await r.json()
    },

    async deleteSource(id) {
      if (!confirm('Remove this source?')) return
      await fetch(`/api/sources/${id}`, { method: 'DELETE' })
      await this.loadSources()
    },
  }
}
```

Include sources list in `index.html`:
```html
<template x-for="src in sources" :key="src.id">
  <div class="card">
    <div style="display:flex;justify-content:space-between;align-items:center">
      <div>
        <strong x-text="src.name"></strong>
        <span class="badge" :class="src.page_count > 0 ? 'badge-ok' : 'badge-pending'"
              x-text="src.page_count + ' pages'"></span>
      </div>
      <div style="display:flex;gap:0.5rem">
        <button class="secondary" @click="connectAuth(src.id)">Connect</button>
        <button class="secondary" @click="crawlNow(src.id)">Crawl Now</button>
        <button class="secondary" style="color:var(--danger)" @click="deleteSource(src.id)">Remove</button>
      </div>
    </div>
    <!-- Device code display modal -->
    <div x-show="deviceCodePending" style="margin-top:1rem;padding:1rem;background:var(--bg);border-radius:6px">
      <p>Visit <strong x-text="deviceCodePending?.verification_uri"></strong> and enter code:</p>
      <p style="font-size:2rem;font-weight:700;letter-spacing:0.2em;color:var(--accent)"
         x-text="deviceCodePending?.user_code"></p>
      <button class="secondary" @click="deviceCodePending=null">Cancel</button>
    </div>
  </div>
</template>
```

**Step 2: Commit**

```bash
git add web/
git commit -m "feat: add sources dashboard with crawl/auth actions"
```

---

### Task 24: Search debug UI

**Files:**
- Modify: `web/static/app.js` and `index.html`

Add search section with query input, results list (title, source, path, score snippet). Useful for verifying that indexing worked correctly.

**Commit:**

```bash
git commit -m "feat: add search debug UI"
```

---

## Phase 9: Main Binary and Wiring

### Task 25: Wire everything together in main.go

**Files:**
- Modify: `cmd/documcp/main.go`

**Step 1: Implement main**

```go
// cmd/documcp/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/documcp/documcp/internal/api"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/crawler"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/embed"
	"github.com/documcp/documcp/internal/mcp"
)

func main() {
	cfgPath := getenv("DOCUMCP_CONFIG", "/app/config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	store, err := db.Open(cfg.Server.DataDir + "/documcp.db")
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	modelPath := getenv("DOCUMCP_MODEL_PATH", "/app/models/all-MiniLM-L6-v2")
	var embedder *embed.Embedder
	if _, err := os.Stat(modelPath); err == nil {
		embedder, err = embed.New(modelPath)
		if err != nil {
			slog.Warn("embedding model not loaded, semantic search disabled", "err", err)
		} else {
			defer embedder.Close()
			slog.Info("embedding model loaded", "path", modelPath)
		}
	}

	c := crawler.New(store, embedder)
	scheduler := crawler.NewScheduler(c, store)
	scheduler.Load(cfg)
	defer scheduler.Stop()

	// Reload config on file change
	config.Watch(cfgPath, func(newCfg *config.Config) {
		slog.Info("config reloaded")
		scheduler.Load(newCfg)
	})

	mcpServer := mcp.NewServer(store, embedder)
	apiServer := api.NewServer(store, c, mcpServer.Handler())

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	slog.Info("starting DocuMcp", "addr", addr)

	srv := &http.Server{Addr: addr, Handler: apiServer}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	slog.Info("shutting down")
	srv.Shutdown(context.Background())
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

**Step 2: Build and verify**

```bash
make build
./bin/documcp
```
Expected: "starting DocuMcp" logged, exits when no config present

**Step 3: Commit**

```bash
git add cmd/documcp/
git commit -m "feat: wire all components together in main binary"
```

---

## Phase 10: Docker

### Task 26: Dockerfile with ONNX model

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.dockerignore`

**Step 1: Create Dockerfile**

```dockerfile
# Dockerfile
FROM golang:1.23-bookworm AS builder

RUN apt-get update && apt-get install -y gcc libsqlite3-dev

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o /documcp ./cmd/documcp

# Download ONNX model during build
FROM python:3.12-slim AS model-downloader
RUN pip install huggingface-hub optimum[onnxruntime]
RUN python -c "
from optimum.exporters.onnx import main_export
main_export('sentence-transformers/all-MiniLM-L6-v2', output='/models/all-MiniLM-L6-v2', task='feature-extraction')
"

# Runtime image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /documcp /usr/local/bin/documcp
COPY --from=model-downloader /models /app/models

RUN mkdir -p /app/data

EXPOSE 8080
VOLUME ["/app/data", "/app/config.yaml"]

CMD ["documcp"]
```

**Step 2: Create docker-compose.yml**

```yaml
services:
  documcp:
    build: .
    image: ghcr.io/documcp/documcp:latest
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml
      - documcp_data:/app/data
      - ~/.azure:/root/.azure:ro
      - ~/.config/gh:/root/.config/gh:ro
    restart: unless-stopped
    environment:
      - DOCUMCP_CONFIG=/app/config.yaml
      - DOCUMCP_MODEL_PATH=/app/models/all-MiniLM-L6-v2

volumes:
  documcp_data:
```

**Step 3: Create .dockerignore**

```
.git
bin/
*.md
docs/
```

**Step 4: Build and test Docker image**

```bash
docker build -t documcp:local .
docker run --rm -p 8080:8080 -v $(pwd)/config.yaml:/app/config.yaml documcp:local
```
Expected: Server starts on :8080, web UI accessible

**Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml .dockerignore
git commit -m "feat: add Dockerfile with embedded ONNX model and docker-compose"
```

---

### Task 27: Makefile build targets and README

**Files:**
- Modify: `Makefile`

```makefile
.PHONY: build test docker run clean

build:
	CGO_ENABLED=1 go build -o bin/documcp ./cmd/documcp

test:
	CGO_ENABLED=1 go test ./... -v -timeout 30s

docker:
	docker build -t documcp:local .

run: docker
	docker-compose up

clean:
	rm -rf bin/
```

**Commit:**

```bash
git add Makefile
git commit -m "chore: add Makefile targets for build, test, docker"
```

---

## Testing Checklist

Before considering implementation complete, verify:

- [ ] `make test` passes with no failures
- [ ] `make build` produces a working binary
- [ ] Binary starts, loads config, and serves on :8080
- [ ] Web UI loads in browser (dark themed)
- [ ] Adding a public web source in UI writes to config.yaml
- [ ] "Crawl Now" indexes pages and updates page count
- [ ] `search_docs` MCP tool returns results after indexing
- [ ] `browse_source` returns sections without a section arg
- [ ] `browse_source` returns pages with a section arg
- [ ] `get_page` returns full content
- [ ] GitHub device code flow shows user_code in UI
- [ ] Azure device code flow shows user_code in UI
- [ ] Docker image builds and runs successfully
- [ ] Token storage is encrypted (verify tokens table contains ciphertext, not plaintext)

---

## Post-Launch Improvements (2026-03-05)

All items committed to `main` branch.

| Change | Commits |
|---|---|
| Security fixes (SSRF, CSP, token encryption, authRevoke) | `4db5197` |
| Alpine.js vendored, script load order fixed, unsafe-eval CSP | `4db5197`, `36827a70` |
| Polite web crawler: 500ms delay, User-Agent, 429/Retry-After | `4b2b0ca` |
| Sitemap discovery: path-local then root fallback | `4b2b0ca` |
| Path-prefix filtering to avoid cross-version crawling | `4b2b0ca` |
| Live crawl progress: N / Total pages badge, 2s polling | `05a1f4a`, `88cd085`, `39ebe8d` |
| `crawl_total` DB column + adapter returns total count | `05a1f4a` |
| Server-side `crawlingIDs` tracking in API server | `88cd085` |
| Search result titles clickable, HTML stripped from snippets | `f6b1b3b` |
| `<title>` tag extraction (longer-side split heuristic) | `f6b1b3b` |
| `noscript`/`iframe` added to skipTags | `f6b1b3b` |
| `include_path` field for web sources (path-prefix filter) | `19142c6`–`f9c4001` |
| `filterURL` helper extracted for testability | `4f44257`, `c6347a6` |
| README, CLAUDE.md, `.github/copilot-instructions.md` | `23fd303`, `803dfe6` |

## Testing Checklist (verified ✅)
- `make test` passes — all packages green
- Container builds with `make docker` → `documcp:local`
- Web UI: sources dashboard, add source form, search view all functional
- Crawl progress badge shows "N / Total pages" live
- Search results: clickable titles, clean snippets, page titles from `<title>` tag
- `include_path` restricts crawl to URL prefix (same-origin validated)
- Harbor Docs (90 pages) indexed and searchable
