# Token-Savings Benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `cmd/bench` tool that measures whether DocuMcp reduces agent token consumption vs. naive `web_search` + HTTP-fetch, on both per-page and per-task levels.

**Architecture:** Single Go binary with subcommands. New `internal/bench/` package, isolated from production code. Two measurements share token-counting and report infrastructure: a per-page diff (option 1) that's deterministic and offline-comparable, and a task benchmark (option 2) that runs Claude Sonnet 4.6 in a tool-use loop with two tool configurations (no-DocuMcp vs. DocuMcp). Outputs land in `bench-results/<timestamp>/`. Companion spec: `docs/plans/2026-04-26-token-savings-benchmark-design.md`.

**Tech Stack:** Go, `github.com/anthropics/anthropic-sdk-go` (new dep), `github.com/modelcontextprotocol/go-sdk` (existing), `golang.org/x/net/html` (new dep), standard library `net/http`, `encoding/json`. Build flags `CGO_ENABLED=1 -tags sqlite_fts5` (matches rest of project).

---

## File Map

| File | Responsibility |
|---|---|
| `cmd/bench/main.go` | CLI entry: subcommand dispatch (`page-diff`, `tasks`, `all`, `sample-urls`) and shared flags |
| `internal/bench/tokens/count.go` | Wraps Anthropic `messages/count_tokens`; injectable HTTP client for tests |
| `internal/bench/pagediff/strip.go` | HTML→text stripper (drops script/style/noscript/iframe; collapses whitespace) |
| `internal/bench/pagediff/pagediff.go` | Per-URL runner: fetch raw, strip, call DocuMcp `get_page`, count tokens |
| `internal/bench/tasks/types.go` | `Question`, `TrialResult`, `RunReport` types shared by tasks/runner and report writers |
| `internal/bench/tasks/corpus.go` | Loads/validates `corpus/questions.json` against running DocuMcp |
| `internal/bench/tasks/config_a.go` | Tool definitions & function-tool handler for Configuration A (`web_search` + `fetch_url`) |
| `internal/bench/tasks/config_b.go` | MCP client + four function-tool wrappers proxying to it (Configuration B) |
| `internal/bench/tasks/runner.go` | Agent tool-use loop with hard limits; trial orchestration |
| `internal/bench/tasks/judge.go` | Correctness judge: separate Claude call, returns `{correct, reason}` |
| `internal/bench/report/stats.go` | Bootstrap CI helper |
| `internal/bench/report/json.go` | Emits machine-readable `results.json` (option 1 + option 2 rows + run metadata) |
| `internal/bench/report/markdown.go` | Emits human-readable `summary.md` |
| `internal/bench/corpus/questions.json` | Single example entry committed; operator fills in the rest |
| `internal/bench/corpus/page-urls.txt` | Empty placeholder; populated by `bench sample-urls` |
| `Makefile` | Add `bench` target |

---

## Task 1: Scaffold cmd/bench and Makefile target

**Files:**
- Create: `cmd/bench/main.go`
- Modify: `Makefile`

- [ ] **Step 1: Write the cmd/bench main.go scaffold**

```go
// cmd/bench/main.go
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "page-diff":
		runPageDiff(os.Args[2:])
	case "tasks":
		runTasks(os.Args[2:])
	case "all":
		runAll(os.Args[2:])
	case "sample-urls":
		runSampleURLs(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`bench — DocuMcp token-savings benchmark

Usage:
  bench page-diff   [--urls FILE]
  bench tasks       [--questions FILE] [--trials N]
  bench all         [--urls FILE] [--questions FILE] [--trials N]
  bench sample-urls --per-source N

Environment:
  ANTHROPIC_API_KEY   Anthropic API key (required for tasks/all)
  DOCUMCP_BENCH_URL   DocuMcp instance URL (default: http://127.0.0.1:8080)
  DOCUMCP_API_KEY     Bearer token if DocuMcp requires auth`)
}

func runPageDiff(_ []string) {
	fmt.Fprintln(os.Stderr, "page-diff: not yet implemented")
	os.Exit(1)
}

func runTasks(_ []string) {
	fmt.Fprintln(os.Stderr, "tasks: not yet implemented")
	os.Exit(1)
}

func runAll(_ []string) {
	fmt.Fprintln(os.Stderr, "all: not yet implemented")
	os.Exit(1)
}

func runSampleURLs(args []string) {
	fs := flag.NewFlagSet("sample-urls", flag.ExitOnError)
	_ = fs.Int("per-source", 5, "max URLs per source")
	_ = fs.Parse(args)
	fmt.Fprintln(os.Stderr, "sample-urls: not yet implemented")
	os.Exit(1)
}
```

- [ ] **Step 2: Add `bench` Makefile target**

Open `Makefile` and add a target near the existing `build` target:

```makefile
.PHONY: bench
bench:
	CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/bench ./cmd/bench
```

- [ ] **Step 3: Verify it builds**

Run: `make bench`
Expected: `bin/bench` produced, no errors.

Run: `./bin/bench --help`
Expected: usage message printed.

Run: `./bin/bench page-diff`
Expected: stderr "page-diff: not yet implemented", exit code 1.

- [ ] **Step 4: Add bench-results to .gitignore**

Append a line to `.gitignore`:

```
bench-results/
bin/bench
```

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/main.go Makefile .gitignore
git commit -m "feat(bench): scaffold cmd/bench with subcommand dispatch"
```

---

## Task 2: Anthropic count_tokens wrapper

**Files:**
- Create: `internal/bench/tokens/count.go`
- Create: `internal/bench/tokens/count_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/tokens/count_test.go
package tokens

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCount_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages/count_tokens" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("missing or wrong api key header: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": 42})
	}))
	defer srv.Close()

	c := New("test-key", "claude-sonnet-4-6", WithBaseURL(srv.URL))
	got, err := c.Count(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 42 {
		t.Fatalf("want 42, got %d", got)
	}
}

func TestCount_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := New("test-key", "claude-sonnet-4-6", WithBaseURL(srv.URL))
	if _, err := c.Count(context.Background(), "x"); err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tokens/...`
Expected: FAIL with "package internal/bench/tokens not found" or "undefined: New".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/tokens/count.go
package tokens

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Counter calls Anthropic's messages/count_tokens endpoint. Free, exact, no rate limit
// concerns for our scale. We prefer it over a third-party tokenizer to avoid drift.
type Counter struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

type Option func(*Counter)

func WithBaseURL(u string) Option { return func(c *Counter) { c.baseURL = u } }

func WithHTTPClient(h *http.Client) Option { return func(c *Counter) { c.client = h } }

func New(apiKey, model string, opts ...Option) *Counter {
	c := &Counter{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type requestBody struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type responseBody struct {
	InputTokens int `json:"input_tokens"`
}

// Count returns the input-token count for the given text wrapped as a single user message.
func (c *Counter) Count(ctx context.Context, text string) (int, error) {
	body, err := json.Marshal(requestBody{
		Model:    c.model,
		Messages: []message{{Role: "user", Content: text}},
	})
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages/count_tokens", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("count_tokens returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var rb responseBody
	if err := json.Unmarshal(respBytes, &rb); err != nil {
		return 0, fmt.Errorf("unmarshal: %w", err)
	}
	return rb.InputTokens, nil
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tokens/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/tokens/
git commit -m "feat(bench): add Anthropic count_tokens wrapper"
```

---

## Task 3: HTML→text stripper

**Files:**
- Create: `internal/bench/pagediff/strip.go`
- Create: `internal/bench/pagediff/strip_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/pagediff/strip_test.go
package pagediff

import (
	"strings"
	"testing"
)

func TestStrip_DropsScriptStyleAndKeepsNav(t *testing.T) {
	in := `<html><head><title>T</title><style>a{}</style></head>
<body><nav>NAVIGATION</nav><script>alert(1)</script>
<p>Hello   world</p><footer>FOOT</footer></body></html>`
	out, err := Strip(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.Contains(out, "alert(1)") {
		t.Errorf("script content leaked: %q", out)
	}
	if strings.Contains(out, "a{}") {
		t.Errorf("style content leaked: %q", out)
	}
	if !strings.Contains(out, "NAVIGATION") {
		t.Errorf("nav text dropped (the naive baseline is supposed to keep it): %q", out)
	}
	if !strings.Contains(out, "FOOT") {
		t.Errorf("footer text dropped: %q", out)
	}
	if !strings.Contains(out, "Hello world") {
		t.Errorf("whitespace not collapsed: %q", out)
	}
}

func TestStrip_HandlesEmptyInput(t *testing.T) {
	out, err := Strip(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestStrip_DropsNoscriptAndIframe(t *testing.T) {
	in := `<noscript>NS</noscript><iframe src=x>IF</iframe><p>P</p>`
	out, err := Strip(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if strings.Contains(out, "NS") || strings.Contains(out, "IF") {
		t.Errorf("noscript/iframe leaked: %q", out)
	}
	if !strings.Contains(out, "P") {
		t.Errorf("paragraph dropped: %q", out)
	}
}
```

- [ ] **Step 2: Add the html dep and run the failing test**

Run: `go get golang.org/x/net/html`
Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/pagediff/...`
Expected: FAIL with "undefined: Strip".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/pagediff/strip.go
package pagediff

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// dropTags name elements whose subtree is omitted entirely. We deliberately do NOT
// drop nav/footer/header/aside — a naive agent fetching raw HTML wouldn't know to.
var dropTags = map[string]bool{
	"script":   true,
	"style":    true,
	"noscript": true,
	"iframe":   true,
}

// Strip parses the HTML and returns a single string containing the visible text,
// with whitespace runs collapsed to a single space.
func Strip(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	var b strings.Builder
	walk(doc, &b)
	return collapseWhitespace(b.String()), nil
}

func walk(n *html.Node, b *strings.Builder) {
	if n.Type == html.ElementNode && dropTags[n.Data] {
		return
	}
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		b.WriteByte(' ')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, b)
	}
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/pagediff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/bench/pagediff/
git commit -m "feat(bench): add HTML→text stripper for naive baseline"
```

---

## Task 4: Page-diff runner

**Files:**
- Create: `internal/bench/pagediff/pagediff.go`
- Create: `internal/bench/pagediff/pagediff_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/pagediff/pagediff_test.go
package pagediff

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRun_AggregatesAndComputesRatios(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/page-a") {
			_, _ = w.Write([]byte("<html><script>x</script><p>aaaa aaaa aaaa aaaa</p></html>"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Stub DocuMcp page fetcher returns short text.
	docFetch := func(_ context.Context, _ string) (string, error) {
		return "doc", nil
	}
	// Stub token counter: returns len(text) so we can predict ratios.
	count := func(_ context.Context, s string) (int, error) {
		return len(s), nil
	}

	got, err := Run(context.Background(), Config{
		URLs:           []string{srv.URL + "/page-a"},
		HTTPClient:     srv.Client(),
		FetchFromDocMc: docFetch,
		CountTokens:    count,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(got.Rows))
	}
	r := got.Rows[0]
	if r.TokensRaw <= r.TokensStripped {
		t.Errorf("raw should be larger than stripped: raw=%d stripped=%d", r.TokensRaw, r.TokensStripped)
	}
	if r.TokensDocuMcp != 3 { // len("doc")
		t.Errorf("expected DocuMcp tokens 3, got %d", r.TokensDocuMcp)
	}
	if got.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", got.Skipped)
	}
}

func TestRun_SkipsFailingURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	count := func(_ context.Context, s string) (int, error) { return len(s), nil }
	docFetch := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("not indexed")
	}

	got, err := Run(context.Background(), Config{
		URLs:           []string{srv.URL + "/missing"},
		HTTPClient:     srv.Client(),
		FetchFromDocMc: docFetch,
		CountTokens:    count,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got.Rows) != 0 {
		t.Errorf("want 0 rows, got %d", len(got.Rows))
	}
	if got.Skipped != 1 {
		t.Errorf("want 1 skipped, got %d", got.Skipped)
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/pagediff/...`
Expected: FAIL with "undefined: Run" / "undefined: Config".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/pagediff/pagediff.go
package pagediff

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// maxBodyBytes mirrors the github_repo adapter's per-file cap. Any body larger
// is treated as a fetch failure (URL is skipped, not truncated).
const maxBodyBytes = 5 * 1024 * 1024

// FetchFn fetches the DocuMcp-extracted text for a given URL. In production this
// proxies to MCP get_page. Injected so tests can stub it.
type FetchFn func(ctx context.Context, url string) (string, error)

// CountFn returns the token count for a given string. In production this calls
// Anthropic count_tokens. Injected so tests can stub it.
type CountFn func(ctx context.Context, text string) (int, error)

type Config struct {
	URLs           []string
	HTTPClient     *http.Client
	FetchFromDocMc FetchFn
	CountTokens    CountFn
}

type Row struct {
	URL                  string  `json:"url"`
	TokensRaw            int     `json:"tokens_raw"`
	TokensStripped       int     `json:"tokens_stripped"`
	TokensDocuMcp        int     `json:"tokens_documcp"`
	RatioStrippedOverDoc float64 `json:"ratio_stripped_over_documcp"`
	RatioRawOverDoc      float64 `json:"ratio_raw_over_documcp"`
}

type Result struct {
	Rows    []Row    `json:"rows"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

func Run(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.FetchFromDocMc == nil {
		return nil, fmt.Errorf("FetchFromDocMc is required")
	}
	if cfg.CountTokens == nil {
		return nil, fmt.Errorf("CountTokens is required")
	}

	out := &Result{Rows: make([]Row, 0, len(cfg.URLs))}
	for _, u := range cfg.URLs {
		row, err := runOne(ctx, cfg, u)
		if err != nil {
			out.Skipped++
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", u, err))
			slog.Warn("page-diff skip", "url", u, "err", err)
			continue
		}
		out.Rows = append(out.Rows, *row)
	}
	return out, nil
}

func runOne(ctx context.Context, cfg Config, url string) (*Row, error) {
	rawBytes, err := fetchRaw(ctx, cfg.HTTPClient, url)
	if err != nil {
		return nil, fmt.Errorf("fetch raw: %w", err)
	}
	rawText := string(rawBytes)
	tRaw, err := cfg.CountTokens(ctx, rawText)
	if err != nil {
		return nil, fmt.Errorf("count raw: %w", err)
	}

	stripped, err := Strip(strings.NewReader(rawText))
	if err != nil {
		return nil, fmt.Errorf("strip: %w", err)
	}
	tStripped, err := cfg.CountTokens(ctx, stripped)
	if err != nil {
		return nil, fmt.Errorf("count stripped: %w", err)
	}

	docText, err := cfg.FetchFromDocMc(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch documcp: %w", err)
	}
	tDoc, err := cfg.CountTokens(ctx, docText)
	if err != nil {
		return nil, fmt.Errorf("count documcp: %w", err)
	}
	if tDoc == 0 {
		return nil, fmt.Errorf("documcp returned empty text")
	}

	return &Row{
		URL:                  url,
		TokensRaw:            tRaw,
		TokensStripped:       tStripped,
		TokensDocuMcp:        tDoc,
		RatioStrippedOverDoc: float64(tStripped) / float64(tDoc),
		RatioRawOverDoc:      float64(tRaw) / float64(tDoc),
	}, nil
}

func fetchRaw(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "DocuMcp-Bench/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/pagediff/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/pagediff/
git commit -m "feat(bench): add page-diff runner with injected fetcher/counter"
```

---

## Task 5: Bootstrap CI helper

**Files:**
- Create: `internal/bench/report/stats.go`
- Create: `internal/bench/report/stats_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/report/stats_test.go
package report

import (
	"math"
	"testing"
)

func TestBootstrapCI95_KnownDistribution(t *testing.T) {
	// 100 samples of value 100; mean=100; CI should be a tight band around 100.
	xs := make([]float64, 100)
	for i := range xs {
		xs[i] = 100
	}
	mean, lo, hi := BootstrapCI95(xs, 1000, 42)
	if mean != 100 {
		t.Errorf("mean: want 100, got %v", mean)
	}
	if lo != 100 || hi != 100 {
		t.Errorf("constant samples should give zero-width CI, got [%v, %v]", lo, hi)
	}
}

func TestBootstrapCI95_VariedDistribution(t *testing.T) {
	xs := []float64{10, 12, 14, 16, 18, 20, 22, 24, 26, 28}
	mean, lo, hi := BootstrapCI95(xs, 1000, 42)
	if math.Abs(mean-19) > 0.001 {
		t.Errorf("mean: want 19, got %v", mean)
	}
	if !(lo < mean && mean < hi) {
		t.Errorf("CI should bracket mean, got [%v, %v] mean %v", lo, hi, mean)
	}
}

func TestBootstrapCI95_EmptyReturnsNaN(t *testing.T) {
	mean, lo, hi := BootstrapCI95(nil, 100, 0)
	if !math.IsNaN(mean) || !math.IsNaN(lo) || !math.IsNaN(hi) {
		t.Errorf("empty samples should yield NaN, got mean=%v lo=%v hi=%v", mean, lo, hi)
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/report/...`
Expected: FAIL with "package not found" or "undefined: BootstrapCI95".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/report/stats.go
package report

import (
	"math"
	"math/rand"
	"sort"
)

// BootstrapCI95 returns the mean of xs and a 95% percentile bootstrap CI computed
// from `resamples` resamples. seed makes the result deterministic for tests.
// Returns NaN for all three values if xs is empty.
func BootstrapCI95(xs []float64, resamples int, seed int64) (mean, lo, hi float64) {
	if len(xs) == 0 {
		nan := math.NaN()
		return nan, nan, nan
	}
	mean = meanOf(xs)
	if resamples <= 0 {
		return mean, mean, mean
	}

	rng := rand.New(rand.NewSource(seed))
	means := make([]float64, resamples)
	tmp := make([]float64, len(xs))
	for i := 0; i < resamples; i++ {
		for j := range tmp {
			tmp[j] = xs[rng.Intn(len(xs))]
		}
		means[i] = meanOf(tmp)
	}
	sort.Float64s(means)
	lo = means[int(0.025*float64(resamples))]
	hi = means[int(0.975*float64(resamples))]
	return mean, lo, hi
}

func meanOf(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/report/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/report/
git commit -m "feat(bench): add 95% percentile bootstrap CI helper"
```

---

## Task 6: Tasks types and corpus loader

**Files:**
- Create: `internal/bench/tasks/types.go`
- Create: `internal/bench/tasks/corpus.go`
- Create: `internal/bench/tasks/corpus_test.go`
- Create: `internal/bench/corpus/questions.json`
- Create: `internal/bench/corpus/page-urls.txt`

- [ ] **Step 1: Write the example questions.json**

```json
[
  {
    "id": "fts5-trigram-tokenizer",
    "tier": 1,
    "question": "What's the exact `tokenize=` value for FTS5's trigram tokenizer with a minimum length of 3?",
    "expected_source": "sqlite-docs",
    "expected_url_pattern": "sqlite\\.org/fts5\\.html",
    "reference_excerpt": "tokenize = 'trigram detail=column case_sensitive 0' — see §4.3.5 trigram tokenizer; tokendata=1 unsupported",
    "notes": "Tier 1 single-fact lookup. Operator: replace this entry with hand-written questions matching your indexed sources."
  }
]
```

- [ ] **Step 2: Write the placeholder page-urls.txt**

```
# Populated by `bench sample-urls --per-source N` after a DocuMcp instance is running.
# One URL per line. Lines starting with # are ignored.
```

- [ ] **Step 3: Write the failing test**

```go
// internal/bench/tasks/corpus_test.go
package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadCorpus_ValidatesFields(t *testing.T) {
	dir := t.TempDir()
	good := writeFile(t, dir, "good.json", `[
		{"id":"q1","tier":1,"question":"q?","expected_source":"src","expected_url_pattern":"x","reference_excerpt":"e"}
	]`)
	qs, err := LoadCorpus(good, map[string]bool{"src": true})
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if len(qs) != 1 || qs[0].ID != "q1" {
		t.Fatalf("unexpected qs: %+v", qs)
	}
}

func TestLoadCorpus_RejectsUnknownSource(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q1","tier":1,"question":"q?","expected_source":"missing","expected_url_pattern":"x","reference_excerpt":"e"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestLoadCorpus_RejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q","tier":1,"question":"a","expected_source":"src","expected_url_pattern":"x","reference_excerpt":"e"},
		{"id":"q","tier":2,"question":"b","expected_source":"src","expected_url_pattern":"y","reference_excerpt":"f"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestLoadCorpus_RejectsBadTier(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q","tier":4,"question":"a","expected_source":"src","expected_url_pattern":"x","reference_excerpt":"e"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for tier 4")
	}
}

func TestLoadCorpus_RejectsBadRegex(t *testing.T) {
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.json", `[
		{"id":"q","tier":1,"question":"a","expected_source":"src","expected_url_pattern":"[unbalanced","reference_excerpt":"e"}
	]`)
	if _, err := LoadCorpus(bad, map[string]bool{"src": true}); err == nil {
		t.Fatal("expected error for bad regex")
	}
}
```

- [ ] **Step 4: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: FAIL with "undefined: LoadCorpus".

- [ ] **Step 5: Write types.go and corpus.go**

```go
// internal/bench/tasks/types.go
package tasks

import "regexp"

// Question is a single benchmark prompt + ground truth.
type Question struct {
	ID                 string         `json:"id"`
	Tier               int            `json:"tier"`
	Question           string         `json:"question"`
	ExpectedSource     string         `json:"expected_source"`
	ExpectedURLPattern string         `json:"expected_url_pattern"`
	ReferenceExcerpt   string         `json:"reference_excerpt"`
	Notes              string         `json:"notes,omitempty"`
	urlRegex           *regexp.Regexp // populated by LoadCorpus
}

// URLRegex returns the compiled expected_url_pattern. Always non-nil after LoadCorpus.
func (q *Question) URLRegex() *regexp.Regexp { return q.urlRegex }

// TrialResult is one (question, config, trial) outcome.
type TrialResult struct {
	QuestionID    string `json:"question_id"`
	Config        string `json:"config"` // "A" or "B"
	Trial         int    `json:"trial"`
	InputTokens   int    `json:"input_tokens"`
	OutputTokens  int    `json:"output_tokens"`
	ToolCalls     int    `json:"tool_calls"`
	Aborted       bool   `json:"aborted"`
	Correct       bool   `json:"correct"`
	JudgeReason   string `json:"judge_reason"`
	FinalAnswer   string `json:"final_answer"`
	CitedURLs     []string `json:"cited_urls"`
}

// TotalTokens is input + output (excludes judge tokens, which are tracked separately).
func (t TrialResult) TotalTokens() int { return t.InputTokens + t.OutputTokens }

// JudgeAccounting tracks judge-only token spend across the run.
type JudgeAccounting struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
```

```go
// internal/bench/tasks/corpus.go
package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// LoadCorpus parses questions.json and validates every entry against knownSources
// (the set of source names returned by the running DocuMcp's GET /api/sources).
// Returns Questions with urlRegex populated.
func LoadCorpus(path string, knownSources map[string]bool) ([]Question, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read corpus: %w", err)
	}
	var qs []Question
	if err := json.Unmarshal(raw, &qs); err != nil {
		return nil, fmt.Errorf("parse corpus: %w", err)
	}
	seen := make(map[string]bool, len(qs))
	for i := range qs {
		q := &qs[i]
		if q.ID == "" {
			return nil, fmt.Errorf("entry %d: id is required", i)
		}
		if seen[q.ID] {
			return nil, fmt.Errorf("duplicate id: %s", q.ID)
		}
		seen[q.ID] = true
		if q.Tier < 1 || q.Tier > 3 {
			return nil, fmt.Errorf("%s: tier must be 1, 2, or 3 (got %d)", q.ID, q.Tier)
		}
		if q.Question == "" {
			return nil, fmt.Errorf("%s: question is required", q.ID)
		}
		if !knownSources[q.ExpectedSource] {
			return nil, fmt.Errorf("%s: expected_source %q not found in DocuMcp instance", q.ID, q.ExpectedSource)
		}
		re, err := regexp.Compile(q.ExpectedURLPattern)
		if err != nil {
			return nil, fmt.Errorf("%s: expected_url_pattern: %w", q.ID, err)
		}
		q.urlRegex = re
		if q.ReferenceExcerpt == "" {
			return nil, fmt.Errorf("%s: reference_excerpt is required", q.ID)
		}
	}
	return qs, nil
}
```

- [ ] **Step 6: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/bench/tasks/ internal/bench/corpus/
git commit -m "feat(bench): add Question/TrialResult types and corpus loader"
```

---

## Task 7: Configuration A — `fetch_url` function-tool handler

**Files:**
- Create: `internal/bench/tasks/config_a.go`
- Create: `internal/bench/tasks/config_a_test.go`

> **Note:** the `web_search` server tool requires no Go-side handler — Anthropic executes it. We only need to declare it in the tool list. The `fetch_url` function tool needs a real handler.

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/tasks/config_a_test.go
package tasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchURL_ReturnsStrippedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><script>x</script><p>Hello world</p></html>"))
	}))
	defer srv.Close()

	out, err := FetchURL(context.Background(), srv.Client(), srv.URL, 50_000)
	if err != nil {
		t.Fatalf("FetchURL: %v", err)
	}
	if !strings.Contains(out, "Hello world") {
		t.Errorf("missing body text: %q", out)
	}
	if strings.Contains(out, "script") {
		t.Errorf("script not stripped: %q", out)
	}
}

func TestFetchURL_TruncatesOverCap(t *testing.T) {
	big := strings.Repeat("a", 200_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body><p>" + big + "</p></body></html>"))
	}))
	defer srv.Close()

	out, err := FetchURL(context.Background(), srv.Client(), srv.URL, 1000)
	if err != nil {
		t.Fatalf("FetchURL: %v", err)
	}
	if !strings.HasSuffix(out, "...[truncated]") {
		t.Errorf("expected truncation marker, got suffix %q", out[max(0, len(out)-30):])
	}
	if len(out) > 1000+len("...[truncated]") {
		t.Errorf("body exceeded cap: %d chars", len(out))
	}
}

func TestFetchURL_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := FetchURL(context.Background(), srv.Client(), srv.URL, 1000); err == nil {
		t.Fatal("expected error on 404")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestFetchURL ./internal/bench/tasks/...`
Expected: FAIL with "undefined: FetchURL".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/tasks/config_a.go
package tasks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
)

// FetchURL is the handler for Configuration A's `fetch_url` function tool.
// Returns stripped text, truncated to maxChars with a "...[truncated]" marker.
func FetchURL(ctx context.Context, client *http.Client, url string, maxChars int) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "DocuMcp-Bench/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	stripped, err := pagediff.Strip(strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("strip: %w", err)
	}
	if len(stripped) > maxChars {
		return stripped[:maxChars] + "...[truncated]", nil
	}
	return stripped, nil
}

// ConfigATools returns the tool list for Configuration A: web_search (server-side)
// + fetch_url (function tool). Returned as a generic []map so callers can pass it
// to whichever Anthropic SDK shape they're using.
func ConfigATools() []map[string]any {
	return []map[string]any{
		{
			"type": "web_search_20250305",
			"name": "web_search",
		},
		{
			"name":        "fetch_url",
			"description": "Fetches the given URL and returns its visible text content. Use this after web_search to read a candidate page.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Absolute URL to fetch.",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/tasks/config_a.go internal/bench/tasks/config_a_test.go
git commit -m "feat(bench): add Configuration A tools (web_search + fetch_url)"
```

---

## Task 8: Configuration B — DocuMcp MCP client and tool wrappers

**Files:**
- Create: `internal/bench/tasks/config_b.go`
- Create: `internal/bench/tasks/config_b_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/tasks/config_b_test.go
package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMCP returns canned MCP-shaped JSON for the four tool calls plus list/tools.
// We reproduce the JSON-RPC shape DocuMcp's /mcp/* endpoint speaks rather than
// pulling in the SDK's full client path — keeps the test fast and explicit.
func fakeMCP() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string                 `json:"method"`
			Params map[string]any         `json:"params"`
			ID     any                    `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("content-type", "application/json")
		switch req.Method {
		case "tools/call":
			name, _ := req.Params["name"].(string)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "stub-output-for-" + name},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestMCPClient_CallTool(t *testing.T) {
	srv := fakeMCP()
	defer srv.Close()

	c := NewMCPClient(srv.URL+"/mcp", "")
	out, err := c.CallTool(context.Background(), "search_docs", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(out, "stub-output-for-search_docs") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestConfigBTools_HasFour(t *testing.T) {
	tools := ConfigBTools()
	want := map[string]bool{"list_sources": true, "search_docs": true, "browse_source": true, "get_page": true}
	if len(tools) != 4 {
		t.Fatalf("want 4 tools, got %d", len(tools))
	}
	for _, tl := range tools {
		name, _ := tl["name"].(string)
		if !want[name] {
			t.Errorf("unexpected tool: %s", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("missing tools: %v", want)
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestMCPClient ./internal/bench/tasks/...`
Expected: FAIL with "undefined: NewMCPClient" / "undefined: ConfigBTools".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/tasks/config_b.go
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// MCPClient is a minimal JSON-RPC client for DocuMcp's /mcp/ endpoint.
// We don't use the full MCP Go SDK here — we only need tools/call, and the
// minimal client keeps the bench tool's blast radius small and easy to test.
type MCPClient struct {
	endpoint string
	bearer   string // optional Authorization Bearer token
	client   *http.Client
	idCount  atomic.Int64
}

func NewMCPClient(endpoint, bearer string) *MCPClient {
	return &MCPClient{
		endpoint: endpoint,
		bearer:   bearer,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

// CallTool invokes the named MCP tool with the given arguments and returns the
// concatenated text content of the response.
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	id := c.idCount.Add(1)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	if c.bearer != "" {
		req.Header.Set("authorization", "Bearer "+c.bearer)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("mcp returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var rb struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &rb); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	if rb.Error != nil {
		return "", fmt.Errorf("mcp error: %s", rb.Error.Message)
	}
	var out strings.Builder
	for _, p := range rb.Result.Content {
		if p.Type == "text" {
			out.WriteString(p.Text)
		}
	}
	return out.String(), nil
}

// ConfigBTools returns the tool list for Configuration B: the four DocuMcp MCP tools
// declared as Anthropic function tools. Tool descriptions are intentionally brief —
// the agent learns by calling them.
func ConfigBTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "list_sources",
			"description": "Lists all documentation sources available in DocuMcp.",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			"name":        "search_docs",
			"description": "Hybrid keyword + semantic search across indexed documentation. Returns ranked excerpts. Optional `source` filters to one source name.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":  map[string]any{"type": "string"},
					"source": map[string]any{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "browse_source",
			"description": "Hierarchical TOC for a source. Without `section`: returns top-level sections. With `section`: returns pages in that section.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source":  map[string]any{"type": "string"},
					"section": map[string]any{"type": "string"},
				},
				"required": []string{"source"},
			},
		},
		{
			"name":        "get_page",
			"description": "Returns the full extracted text of the page at the given URL.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{"type": "string"},
				},
				"required": []string{"url"},
			},
		},
	}
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/tasks/config_b.go internal/bench/tasks/config_b_test.go
git commit -m "feat(bench): add Configuration B MCP client and tool wrappers"
```

---

## Task 9: Agent runner with tool-use loop

**Files:**
- Create: `internal/bench/tasks/runner.go`
- Create: `internal/bench/tasks/runner_test.go`

> **Note:** we model the Anthropic API as an injected interface so tests can replay scripted responses. The production wiring uses raw `net/http` + JSON, same shape as `tokens/count.go` — no SDK dependency to manage.

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/tasks/runner_test.go
package tasks

import (
	"context"
	"errors"
	"testing"
)

// scriptedAPI returns scripted responses in order. Each call consumes one entry.
type scriptedAPI struct {
	responses []apiResponse
	calls     int
}

func (s *scriptedAPI) Send(_ context.Context, _ []map[string]any, _ []map[string]any) (apiResponse, error) {
	if s.calls >= len(s.responses) {
		return apiResponse{}, errors.New("script exhausted")
	}
	r := s.responses[s.calls]
	s.calls++
	return r, nil
}

func TestRunner_StopsOnEndTurn(t *testing.T) {
	api := &scriptedAPI{
		responses: []apiResponse{
			{
				StopReason:   "end_turn",
				InputTokens:  100,
				OutputTokens: 20,
				FinalText:    "the answer is 42",
			},
		},
	}
	tools := map[string]ToolHandler{}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "what's the answer?", RunLimits{MaxRounds: 5})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if res.Aborted {
		t.Errorf("should not have aborted")
	}
	if res.InputTokens != 100 || res.OutputTokens != 20 {
		t.Errorf("token totals: got input=%d output=%d", res.InputTokens, res.OutputTokens)
	}
	if res.FinalAnswer != "the answer is 42" {
		t.Errorf("final answer: %q", res.FinalAnswer)
	}
}

func TestRunner_ExecutesToolThenAnswers(t *testing.T) {
	api := &scriptedAPI{
		responses: []apiResponse{
			{
				StopReason:   "tool_use",
				InputTokens:  50,
				OutputTokens: 10,
				ToolCalls: []toolCall{
					{ID: "t1", Name: "fetch_url", Input: map[string]any{"url": "https://docs.example.com/x"}},
				},
			},
			{
				StopReason:   "end_turn",
				InputTokens:  60,
				OutputTokens: 5,
				FinalText:    "Source: https://docs.example.com/x — answer.",
			},
		},
	}
	tools := map[string]ToolHandler{
		"fetch_url": func(_ context.Context, _ map[string]any) (string, error) { return "page text", nil },
	}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "q?", RunLimits{MaxRounds: 5})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if res.ToolCalls != 1 {
		t.Errorf("tool calls: got %d", res.ToolCalls)
	}
	if res.InputTokens != 110 || res.OutputTokens != 15 {
		t.Errorf("token totals: got input=%d output=%d", res.InputTokens, res.OutputTokens)
	}
	if len(res.CitedURLs) != 1 || res.CitedURLs[0] != "https://docs.example.com/x" {
		t.Errorf("expected one cited url, got %v", res.CitedURLs)
	}
}

func TestRunner_AbortsOnMaxRounds(t *testing.T) {
	// Always returns tool_use, never end_turn.
	loopResp := apiResponse{
		StopReason:   "tool_use",
		InputTokens:  10,
		OutputTokens: 1,
		ToolCalls:    []toolCall{{ID: "t", Name: "fetch_url", Input: map[string]any{"url": "https://x"}}},
	}
	api := &scriptedAPI{responses: []apiResponse{loopResp, loopResp, loopResp, loopResp}}
	tools := map[string]ToolHandler{
		"fetch_url": func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
	}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "q?", RunLimits{MaxRounds: 2})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if !res.Aborted {
		t.Error("expected aborted=true after MaxRounds")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestRunner ./internal/bench/tasks/...`
Expected: FAIL with "undefined: RunTrial" / etc.

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/tasks/runner.go
package tasks

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// API is the minimum surface the runner needs. Production: anthropicAPI{}.
// Tests: scriptedAPI{}. system prompt is fixed by the runner.
type API interface {
	Send(ctx context.Context, tools []map[string]any, messages []map[string]any) (apiResponse, error)
}

type apiResponse struct {
	StopReason   string     // "end_turn", "tool_use", "max_tokens", ...
	InputTokens  int
	OutputTokens int
	ToolCalls    []toolCall // populated if StopReason == "tool_use"
	FinalText    string     // populated if StopReason == "end_turn"
}

type toolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolHandler executes a function tool locally and returns the text result.
// Server tools (e.g. web_search) return ("", nil) — the API handles them.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

type RunLimits struct {
	MaxRounds       int           // default 15
	PerCallTimeout  time.Duration // default 30s
	OverallTimeout  time.Duration // default 5m
}

const systemPrompt = "You are answering a documentation question. Use the available tools to find the answer. Cite the URL of the page where you found the information. Keep your final answer concise — quote or paraphrase only what's needed to answer."

// urlRe finds bare URLs in the agent's final answer; we use them as cited URLs.
var urlRe = regexp.MustCompile(`https?://[^\s<>"')]+`)

// RunTrial executes one (question, config) trial. Returns a TrialResult with
// token totals and the final answer. Correctness is set by the judge later.
func RunTrial(ctx context.Context, api API, handlers map[string]ToolHandler, tools []map[string]any, question string, lim RunLimits) (TrialResult, error) {
	if lim.MaxRounds == 0 {
		lim.MaxRounds = 15
	}
	if lim.PerCallTimeout == 0 {
		lim.PerCallTimeout = 30 * time.Second
	}
	if lim.OverallTimeout == 0 {
		lim.OverallTimeout = 5 * time.Minute
	}
	overallCtx, cancel := context.WithTimeout(ctx, lim.OverallTimeout)
	defer cancel()

	messages := []map[string]any{
		{"role": "user", "content": question},
	}
	res := TrialResult{}
	for round := 0; round < lim.MaxRounds; round++ {
		resp, err := api.Send(overallCtx, tools, prependSystem(messages))
		if err != nil {
			return res, fmt.Errorf("api send: %w", err)
		}
		res.InputTokens += resp.InputTokens
		res.OutputTokens += resp.OutputTokens

		if resp.StopReason == "end_turn" {
			res.FinalAnswer = resp.FinalText
			res.CitedURLs = urlRe.FindAllString(resp.FinalText, -1)
			return res, nil
		}
		if resp.StopReason != "tool_use" {
			res.Aborted = true
			return res, nil
		}

		// Build the assistant message that produced these tool calls. We must echo
		// the tool_use blocks back so the API can match them to the tool_results.
		// Any text the model emitted alongside the tool calls is preserved.
		assistantContent := []map[string]any{}
		if resp.FinalText != "" {
			assistantContent = append(assistantContent, map[string]any{
				"type": "text", "text": resp.FinalText,
			})
		}
		for _, tc := range resp.ToolCalls {
			assistantContent = append(assistantContent, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": tc.Input,
			})
		}

		// Execute each tool call, build the matching tool_result blocks.
		toolResults := []map[string]any{}
		for _, tc := range resp.ToolCalls {
			res.ToolCalls++
			h, ok := handlers[tc.Name]
			var (
				out  string
				ferr error
			)
			if ok {
				callCtx, cancelCall := context.WithTimeout(overallCtx, lim.PerCallTimeout)
				out, ferr = h(callCtx, tc.Input)
				cancelCall()
			}
			toolResults = append(toolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tc.ID,
				"content":     toolText(out, ferr),
				"is_error":    ferr != nil,
			})
		}
		messages = append(messages,
			map[string]any{"role": "assistant", "content": assistantContent},
			map[string]any{"role": "user", "content": toolResults},
		)
	}
	res.Aborted = true
	return res, nil
}

func toolText(out string, err error) string {
	if err != nil {
		return fmt.Sprintf("tool error: %v", err)
	}
	return out
}

func prependSystem(msgs []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(msgs)+1)
	out = append(out, map[string]any{"role": "system", "content": systemPrompt})
	return append(out, msgs...)
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/tasks/runner.go internal/bench/tasks/runner_test.go
git commit -m "feat(bench): add agent tool-use loop with hard limits"
```

---

## Task 10: Production Anthropic API client

**Files:**
- Create: `internal/bench/tasks/api.go`
- Create: `internal/bench/tasks/api_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/tasks/api_test.go
package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicAPI_ParsesEndTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  150,
				"output_tokens": 25,
			},
			"content": []map[string]any{
				{"type": "text", "text": "Done. See https://x.example.com/p."},
			},
		})
	}))
	defer srv.Close()

	a := NewAnthropicAPI("k", "claude-sonnet-4-6", WithAPIBaseURL(srv.URL))
	resp, err := a.Send(context.Background(), nil, []map[string]any{
		{"role": "user", "content": "hi"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.StopReason != "end_turn" || resp.InputTokens != 150 || resp.OutputTokens != 25 {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.FinalText == "" {
		t.Errorf("expected final text")
	}
}

func TestAnthropicAPI_ParsesToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "tool_use",
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 2,
			},
			"content": []map[string]any{
				{"type": "tool_use", "id": "id1", "name": "fetch_url", "input": map[string]any{"url": "u"}},
			},
		})
	}))
	defer srv.Close()

	a := NewAnthropicAPI("k", "claude-sonnet-4-6", WithAPIBaseURL(srv.URL))
	resp, err := a.Send(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "fetch_url" {
		t.Errorf("unexpected tool calls: %+v", resp.ToolCalls)
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestAnthropicAPI ./internal/bench/tasks/...`
Expected: FAIL with "undefined: NewAnthropicAPI".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/tasks/api.go
package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicAPI is the production implementation of the API interface.
type AnthropicAPI struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

type APIOpt func(*AnthropicAPI)

func WithAPIBaseURL(u string) APIOpt    { return func(a *AnthropicAPI) { a.baseURL = u } }
func WithAPIClient(h *http.Client) APIOpt { return func(a *AnthropicAPI) { a.client = h } }

func NewAnthropicAPI(apiKey, model string, opts ...APIOpt) *AnthropicAPI {
	a := &AnthropicAPI{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func (a *AnthropicAPI) Send(ctx context.Context, tools []map[string]any, messages []map[string]any) (apiResponse, error) {
	// Pull system message out of the messages array — Anthropic API takes it as a top-level field.
	system, msgs := splitSystem(messages)

	payload := map[string]any{
		"model":      a.model,
		"max_tokens": 4096,
		"messages":   msgs,
	}
	if system != "" {
		payload["system"] = system
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return apiResponse{}, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return apiResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return apiResponse{}, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiResponse{}, fmt.Errorf("read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return apiResponse{}, fmt.Errorf("messages returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var rb struct {
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBytes, &rb); err != nil {
		return apiResponse{}, fmt.Errorf("unmarshal: %w", err)
	}

	out := apiResponse{
		StopReason:   rb.StopReason,
		InputTokens:  rb.Usage.InputTokens,
		OutputTokens: rb.Usage.OutputTokens,
	}
	var textParts []string
	for _, c := range rb.Content {
		switch c.Type {
		case "text":
			textParts = append(textParts, c.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, toolCall{ID: c.ID, Name: c.Name, Input: c.Input})
		}
	}
	out.FinalText = strings.Join(textParts, "\n")
	return out, nil
}

func splitSystem(messages []map[string]any) (string, []map[string]any) {
	if len(messages) == 0 {
		return "", messages
	}
	if role, _ := messages[0]["role"].(string); role == "system" {
		sys, _ := messages[0]["content"].(string)
		return sys, messages[1:]
	}
	return "", messages
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/tasks/api.go internal/bench/tasks/api_test.go
git commit -m "feat(bench): add production Anthropic Messages API client"
```

---

## Task 11: Correctness judge

**Files:**
- Create: `internal/bench/tasks/judge.go`
- Create: `internal/bench/tasks/judge_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/tasks/judge_test.go
package tasks

import (
	"context"
	"errors"
	"testing"
)

type judgeAPI struct {
	resp apiResponse
	err  error
}

func (j *judgeAPI) Send(_ context.Context, _ []map[string]any, _ []map[string]any) (apiResponse, error) {
	return j.resp, j.err
}

func TestJudge_ParsesCorrectTrue(t *testing.T) {
	j := &judgeAPI{resp: apiResponse{
		StopReason:   "end_turn",
		InputTokens:  30,
		OutputTokens: 10,
		FinalText:    `{"correct": true, "reason": "matches reference"}`,
	}}
	res, judgeIn, judgeOut, err := Judge(context.Background(), j, JudgeInput{
		Question:         "q",
		Answer:           "a",
		ReferenceExcerpt: "r",
		FetchedSources:   "src",
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if !res.Correct || res.Reason != "matches reference" {
		t.Errorf("unexpected: %+v", res)
	}
	if judgeIn != 30 || judgeOut != 10 {
		t.Errorf("token accounting: in=%d out=%d", judgeIn, judgeOut)
	}
}

func TestJudge_HandlesMalformedJSON(t *testing.T) {
	j := &judgeAPI{resp: apiResponse{
		StopReason:   "end_turn",
		InputTokens:  5,
		OutputTokens: 5,
		FinalText:    "not json at all",
	}}
	_, _, _, err := Judge(context.Background(), j, JudgeInput{})
	if err == nil {
		t.Error("expected error on malformed judge response")
	}
}

func TestJudge_PropagatesAPIError(t *testing.T) {
	j := &judgeAPI{err: errors.New("boom")}
	_, _, _, err := Judge(context.Background(), j, JudgeInput{})
	if err == nil {
		t.Error("expected error propagation")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 -run TestJudge ./internal/bench/tasks/...`
Expected: FAIL with "undefined: Judge".

- [ ] **Step 3: Write the implementation**

```go
// internal/bench/tasks/judge.go
package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

type JudgeInput struct {
	Question         string
	Answer           string
	ReferenceExcerpt string
	FetchedSources   string // text content of cited URLs (joined with separators)
}

type JudgeResult struct {
	Correct bool   `json:"correct"`
	Reason  string `json:"reason"`
}

const judgePrompt = `You are evaluating whether a documentation-lookup answer is correct.

QUESTION: %s

REFERENCE EXCERPT (ground truth): %s

CITED SOURCE CONTENT: %s

ANSWER UNDER EVALUATION: %s

Reply with a single JSON object on one line: {"correct": <bool>, "reason": "<short justification>"}. The answer is "correct" if it factually matches the reference, even if phrased differently. The answer is "incorrect" if it contradicts, omits the asked-for fact, or fabricates.`

// jsonObjRe is intentionally tolerant — the judge sometimes wraps its JSON in
// prose. We grab the first {...} block and parse it.
var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// Judge runs the judge call. Returns the result plus judge token counts (so the
// runner can keep them out of the per-config totals).
func Judge(ctx context.Context, api API, in JudgeInput) (JudgeResult, int, int, error) {
	prompt := fmt.Sprintf(judgePrompt, in.Question, in.ReferenceExcerpt, in.FetchedSources, in.Answer)
	resp, err := api.Send(ctx, nil, []map[string]any{
		{"role": "user", "content": prompt},
	})
	if err != nil {
		return JudgeResult{}, 0, 0, fmt.Errorf("judge api: %w", err)
	}
	match := jsonObjRe.FindString(resp.FinalText)
	if match == "" {
		return JudgeResult{}, resp.InputTokens, resp.OutputTokens, fmt.Errorf("no JSON object in judge output: %q", resp.FinalText)
	}
	var r JudgeResult
	if err := json.Unmarshal([]byte(match), &r); err != nil {
		return JudgeResult{}, resp.InputTokens, resp.OutputTokens, fmt.Errorf("parse judge json: %w (raw: %s)", err, match)
	}
	return r, resp.InputTokens, resp.OutputTokens, nil
}
```

- [ ] **Step 4: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/tasks/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/bench/tasks/judge.go internal/bench/tasks/judge_test.go
git commit -m "feat(bench): add LLM-judged correctness evaluator"
```

---

## Task 12: Report writers (JSON + markdown)

**Files:**
- Create: `internal/bench/report/json.go`
- Create: `internal/bench/report/markdown.go`
- Create: `internal/bench/report/report_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/bench/report/report_test.go
package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func TestWriteJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	rep := Report{
		Metadata: Metadata{Model: "claude-sonnet-4-6", DocuMcpVersion: "test"},
		PageDiff: &pagediff.Result{Rows: []pagediff.Row{{URL: "u", TokensRaw: 100, TokensStripped: 50, TokensDocuMcp: 10, RatioStrippedOverDoc: 5, RatioRawOverDoc: 10}}},
		Trials: []tasks.TrialResult{
			{QuestionID: "q1", Config: "A", Trial: 1, InputTokens: 100, OutputTokens: 50, Correct: true},
			{QuestionID: "q1", Config: "B", Trial: 1, InputTokens: 30, OutputTokens: 10, Correct: true},
		},
	}
	if err := WriteJSON(filepath.Join(dir, "results.json"), rep); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "results.json"))
	var got Report
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Metadata.Model != "claude-sonnet-4-6" {
		t.Errorf("metadata round-trip failed: %+v", got.Metadata)
	}
	if len(got.Trials) != 2 {
		t.Errorf("trials round-trip failed: got %d", len(got.Trials))
	}
}

func TestWriteMarkdown_IncludesHeadlineAndPerTier(t *testing.T) {
	dir := t.TempDir()
	rep := Report{
		Metadata: Metadata{Model: "claude-sonnet-4-6"},
		Trials: []tasks.TrialResult{
			{QuestionID: "q1", Config: "A", Trial: 1, InputTokens: 200, OutputTokens: 50, Correct: true},
			{QuestionID: "q1", Config: "B", Trial: 1, InputTokens: 30, OutputTokens: 10, Correct: true},
			{QuestionID: "q1", Config: "A", Trial: 2, InputTokens: 220, OutputTokens: 60, Correct: true},
			{QuestionID: "q1", Config: "B", Trial: 2, InputTokens: 35, OutputTokens: 12, Correct: true},
		},
		Tiers: map[string]int{"q1": 1},
	}
	path := filepath.Join(dir, "summary.md")
	if err := WriteMarkdown(path, rep); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	for _, want := range []string{"Headline", "Config A", "Config B", "Tier 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, s)
		}
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/report/...`
Expected: FAIL with "undefined: Report" / "undefined: WriteJSON" / "undefined: WriteMarkdown".

- [ ] **Step 3: Write json.go**

```go
// internal/bench/report/json.go
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

type Metadata struct {
	Model          string    `json:"model"`
	DocuMcpVersion string    `json:"documcp_version"`
	GitSHA         string    `json:"git_sha"`
	CorpusHash     string    `json:"corpus_hash"`
	Timestamp      time.Time `json:"timestamp"`
}

type Report struct {
	Metadata Metadata             `json:"metadata"`
	PageDiff *pagediff.Result     `json:"page_diff,omitempty"`
	Trials   []tasks.TrialResult  `json:"trials,omitempty"`
	Tiers    map[string]int       `json:"tiers,omitempty"` // questionID → tier
	Judge    tasks.JudgeAccounting `json:"judge"`
}

func WriteJSON(path string, rep Report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Write markdown.go**

```go
// internal/bench/report/markdown.go
package report

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func WriteMarkdown(path string, rep Report) error {
	var b strings.Builder
	b.WriteString("# DocuMcp Token-Savings Benchmark\n\n")
	fmt.Fprintf(&b, "_Model: %s — Generated: %s_\n\n",
		rep.Metadata.Model, rep.Metadata.Timestamp.Format("2006-01-02 15:04:05 MST"))

	writeHeadline(&b, rep.Trials)
	writePerTier(&b, rep.Trials, rep.Tiers)
	writePageDiffTable(&b, rep)
	writeRates(&b, rep.Trials)
	writeJudgeCost(&b, rep.Judge)
	writeSkippedLog(&b, rep)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeHeadline(b *strings.Builder, trials []tasks.TrialResult) {
	a := totalsForConfig(trials, "A")
	bb := totalsForConfig(trials, "B")
	b.WriteString("## Headline\n\n")
	b.WriteString("| Config | N (correct) | Mean tokens | 95% CI |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(b, "| Config A (no DocuMcp) | %d | %.0f | [%.0f, %.0f] |\n", a.n, a.mean, a.lo, a.hi)
	fmt.Fprintf(b, "| Config B (DocuMcp)    | %d | %.0f | [%.0f, %.0f] |\n", bb.n, bb.mean, bb.lo, bb.hi)
	if a.mean > 0 && bb.mean > 0 {
		fmt.Fprintf(b, "\n**DocuMcp savings: %.1f%%**\n\n", 100*(a.mean-bb.mean)/a.mean)
	}
}

func writePerTier(b *strings.Builder, trials []tasks.TrialResult, tiers map[string]int) {
	if len(tiers) == 0 {
		return
	}
	b.WriteString("## Per-Tier Breakdown\n\n")
	b.WriteString("| Tier | Config | N | Mean tokens | 95% CI |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for tier := 1; tier <= 3; tier++ {
		filtered := make([]tasks.TrialResult, 0)
		for _, t := range trials {
			if tiers[t.QuestionID] == tier {
				filtered = append(filtered, t)
			}
		}
		ta := totalsForConfig(filtered, "A")
		tb := totalsForConfig(filtered, "B")
		fmt.Fprintf(b, "| Tier %d | A | %d | %.0f | [%.0f, %.0f] |\n", tier, ta.n, ta.mean, ta.lo, ta.hi)
		fmt.Fprintf(b, "| Tier %d | B | %d | %.0f | [%.0f, %.0f] |\n", tier, tb.n, tb.mean, tb.lo, tb.hi)
	}
	b.WriteByte('\n')
}

func writePageDiffTable(b *strings.Builder, rep Report) {
	if rep.PageDiff == nil || len(rep.PageDiff.Rows) == 0 {
		return
	}
	rows := make([]int, len(rep.PageDiff.Rows))
	for i := range rows {
		rows[i] = i
	}
	sort.Slice(rows, func(i, j int) bool {
		return rep.PageDiff.Rows[rows[i]].RatioStrippedOverDoc > rep.PageDiff.Rows[rows[j]].RatioStrippedOverDoc
	})
	b.WriteString("## Per-Page Token Diff (top 10 by stripped/DocuMcp ratio)\n\n")
	b.WriteString("| URL | tokens_raw | tokens_stripped | tokens_documcp | stripped/doc | raw/doc |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	limit := 10
	if len(rows) < limit {
		limit = len(rows)
	}
	for _, idx := range rows[:limit] {
		r := rep.PageDiff.Rows[idx]
		fmt.Fprintf(b, "| `%s` | %d | %d | %d | %.1f× | %.1f× |\n", r.URL, r.TokensRaw, r.TokensStripped, r.TokensDocuMcp, r.RatioStrippedOverDoc, r.RatioRawOverDoc)
	}
	b.WriteByte('\n')
}

func writeRates(b *strings.Builder, trials []tasks.TrialResult) {
	a := ratesForConfig(trials, "A")
	bb := ratesForConfig(trials, "B")
	b.WriteString("## Correctness & Aborts\n\n")
	b.WriteString("| Config | Correct rate | Mean tool calls | Abort rate |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(b, "| A | %.0f%% | %.1f | %.0f%% |\n", 100*a.correct, a.meanTools, 100*a.aborts)
	fmt.Fprintf(b, "| B | %.0f%% | %.1f | %.0f%% |\n\n", 100*bb.correct, bb.meanTools, 100*bb.aborts)
}

func writeJudgeCost(b *strings.Builder, j tasks.JudgeAccounting) {
	b.WriteString("## Judge Token Cost\n\n")
	fmt.Fprintf(b, "Input: %d — Output: %d (excluded from per-config totals.)\n\n", j.InputTokens, j.OutputTokens)
}

func writeSkippedLog(b *strings.Builder, rep Report) {
	if rep.PageDiff != nil && len(rep.PageDiff.Errors) > 0 {
		b.WriteString("## Page-Diff Skipped URLs\n\n")
		for _, e := range rep.PageDiff.Errors {
			fmt.Fprintf(b, "- %s\n", e)
		}
		b.WriteByte('\n')
	}
	aborted := 0
	for _, t := range rep.Trials {
		if t.Aborted {
			aborted++
		}
	}
	if aborted > 0 {
		fmt.Fprintf(b, "## Aborted Trials\n\n%d trials hit a hard limit (excluded from headline mean).\n\n", aborted)
	}
}

type configTotals struct {
	n        int
	mean     float64
	lo, hi   float64
}

func totalsForConfig(trials []tasks.TrialResult, cfg string) configTotals {
	var xs []float64
	for _, t := range trials {
		if t.Config == cfg && t.Correct && !t.Aborted {
			xs = append(xs, float64(t.TotalTokens()))
		}
	}
	mean, lo, hi := BootstrapCI95(xs, 10000, 1)
	return configTotals{n: len(xs), mean: mean, lo: lo, hi: hi}
}

type configRates struct {
	correct, aborts, meanTools float64
}

func ratesForConfig(trials []tasks.TrialResult, cfg string) configRates {
	var (
		total, ok, aborts, tools int
	)
	for _, t := range trials {
		if t.Config != cfg {
			continue
		}
		total++
		if t.Correct {
			ok++
		}
		if t.Aborted {
			aborts++
		}
		tools += t.ToolCalls
	}
	if total == 0 {
		return configRates{}
	}
	return configRates{
		correct:   float64(ok) / float64(total),
		aborts:    float64(aborts) / float64(total),
		meanTools: float64(tools) / float64(total),
	}
}
```

- [ ] **Step 5: Run tests**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./internal/bench/report/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/bench/report/
git commit -m "feat(bench): add JSON + markdown report writers"
```

---

## Task 13: Wire `bench page-diff` and `bench sample-urls` subcommands

**Files:**
- Modify: `cmd/bench/main.go`
- Create: `cmd/bench/pagediff_cmd.go`
- Create: `cmd/bench/sample_cmd.go`

- [ ] **Step 1: Implement runPageDiff**

Replace the stub `runPageDiff` in `cmd/bench/main.go` with a delegation to `cmd/bench/pagediff_cmd.go`. Then create that file:

```go
// cmd/bench/pagediff_cmd.go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/pagediff"
	"github.com/mathwro/DocuMcp/internal/bench/report"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
	"github.com/mathwro/DocuMcp/internal/bench/tokens"
)

func runPageDiff(args []string) {
	fs := flag.NewFlagSet("page-diff", flag.ExitOnError)
	urlsPath := fs.String("urls", "internal/bench/corpus/page-urls.txt", "path to URL list")
	_ = fs.Parse(args)

	apiKey := mustEnv("ANTHROPIC_API_KEY")
	docURL := envOr("DOCUMCP_BENCH_URL", "http://127.0.0.1:8080")
	bearer := os.Getenv("DOCUMCP_API_KEY")

	urls, err := loadURLs(*urlsPath)
	if err != nil {
		fatal("load urls: %v", err)
	}
	if len(urls) == 0 {
		fatal("no URLs in %s — did you run `bench sample-urls` and commit the result?", *urlsPath)
	}

	counter := tokens.New(apiKey, "claude-sonnet-4-6")
	mcp := tasks.NewMCPClient(docURL+"/mcp", bearer)
	ctx := context.Background()

	res, err := pagediff.Run(ctx, pagediff.Config{
		URLs: urls,
		FetchFromDocMc: func(ctx context.Context, url string) (string, error) {
			return mcp.CallTool(ctx, "get_page", map[string]any{"url": url})
		},
		CountTokens: counter.Count,
	})
	if err != nil {
		fatal("page-diff: %v", err)
	}

	dir := newOutputDir()
	rep := report.Report{
		Metadata: report.Metadata{Model: "claude-sonnet-4-6", Timestamp: time.Now().UTC()},
		PageDiff: res,
	}
	if err := report.WriteJSON(filepath.Join(dir, "results.json"), rep); err != nil {
		fatal("write json: %v", err)
	}
	if err := report.WriteMarkdown(filepath.Join(dir, "summary.md"), rep); err != nil {
		fatal("write md: %v", err)
	}
	fmt.Printf("page-diff complete: %d rows, %d skipped — output: %s\n", len(res.Rows), res.Skipped, dir)
}

func loadURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

func newOutputDir() string {
	dir := filepath.Join("bench-results", time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal("mkdir: %v", err)
	}
	return dir
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fatal("required env var %s not set", k)
	}
	return v
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "bench: "+format+"\n", a...)
	os.Exit(1)
}
```

- [ ] **Step 2: Implement runSampleURLs**

```go
// cmd/bench/sample_cmd.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func runSampleURLs(args []string) {
	fs := flag.NewFlagSet("sample-urls", flag.ExitOnError)
	perSource := fs.Int("per-source", 5, "max URLs per source")
	out := fs.String("out", "internal/bench/corpus/page-urls.txt", "output file")
	_ = fs.Parse(args)

	docURL := envOr("DOCUMCP_BENCH_URL", "http://127.0.0.1:8080")
	bearer := os.Getenv("DOCUMCP_API_KEY")

	sources, err := fetchSources(docURL, bearer)
	if err != nil {
		fatal("list sources: %v", err)
	}
	mcp := tasks.NewMCPClient(docURL+"/mcp", bearer)
	ctx := context.Background()

	var lines []string
	lines = append(lines, "# Generated by `bench sample-urls`. Edit freely; lines starting with # are ignored.")
	for _, src := range sources {
		urls, err := sampleURLsFromSource(ctx, mcp, src, *perSource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip source %s: %v\n", src, err)
			continue
		}
		if len(urls) > 0 {
			lines = append(lines, "", fmt.Sprintf("# %s", src))
			lines = append(lines, urls...)
		}
	}
	if err := os.WriteFile(*out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		fatal("write %s: %v", *out, err)
	}
	fmt.Printf("wrote %s with URLs from %d sources\n", *out, len(sources))
}

func fetchSources(baseURL, bearer string) ([]string, error) {
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/sources", nil)
	if bearer != "" {
		req.Header.Set("authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var raw []struct {
		Name string
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		out = append(out, s.Name)
	}
	return out, nil
}

// sampleURLsFromSource walks browse_source for top-level sections and pulls
// up to perSource page URLs across them.
func sampleURLsFromSource(ctx context.Context, mcp *tasks.MCPClient, source string, perSource int) ([]string, error) {
	root, err := mcp.CallTool(ctx, "browse_source", map[string]any{"source": source})
	if err != nil {
		return nil, fmt.Errorf("browse root: %w", err)
	}
	sections := extractSectionNames(root)
	var urls []string
	for _, sec := range sections {
		if len(urls) >= perSource {
			break
		}
		body, err := mcp.CallTool(ctx, "browse_source", map[string]any{"source": source, "section": sec})
		if err != nil {
			continue
		}
		urls = append(urls, extractURLs(body)...)
	}
	if len(urls) > perSource {
		urls = urls[:perSource]
	}
	return urls, nil
}

// extractSectionNames pulls section names from the browse_source root response.
// browse_source returns text content; we look for lines that look like section labels.
// Operator can adjust this if it doesn't match their data — sample-urls is a one-shot
// helper, not a hot path.
func extractSectionNames(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "http") {
			continue
		}
		// Heuristic: section labels are short, no spaces in front, and not URLs.
		if len(line) < 80 && !strings.Contains(line, "://") {
			out = append(out, line)
		}
	}
	return out
}

// extractURLs pulls http(s) URLs out of a browse_source page-listing response.
func extractURLs(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				out = append(out, fields[0])
			}
		}
	}
	return out
}
```

- [ ] **Step 3: Verify build**

Run: `make bench`
Expected: build succeeds.

- [ ] **Step 4: Smoke-test page-diff with no env**

Run: `./bin/bench page-diff`
Expected: stderr "bench: required env var ANTHROPIC_API_KEY not set", exit 1.

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/
git commit -m "feat(bench): wire page-diff and sample-urls subcommands"
```

---

## Task 14: Wire `bench tasks` subcommand

**Files:**
- Create: `cmd/bench/tasks_cmd.go`
- Modify: `cmd/bench/main.go` (the `runTasks` stub already dispatches by name)

- [ ] **Step 1: Write the implementation**

```go
// cmd/bench/tasks_cmd.go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/report"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func runTasks(args []string) {
	fs := flag.NewFlagSet("tasks", flag.ExitOnError)
	corpusPath := fs.String("questions", "internal/bench/corpus/questions.json", "path to questions.json")
	trials := fs.Int("trials", 3, "trials per (question, config)")
	_ = fs.Parse(args)

	apiKey := mustEnv("ANTHROPIC_API_KEY")
	docURL := envOr("DOCUMCP_BENCH_URL", "http://127.0.0.1:8080")
	bearer := os.Getenv("DOCUMCP_API_KEY")
	ctx := context.Background()

	known, err := fetchSourceSet(docURL, bearer)
	if err != nil {
		fatal("fetch sources: %v", err)
	}
	corpus, err := tasks.LoadCorpus(*corpusPath, known)
	if err != nil {
		fatal("corpus: %v", err)
	}
	if len(corpus) == 0 {
		fatal("corpus is empty")
	}

	api := tasks.NewAnthropicAPI(apiKey, "claude-sonnet-4-6")
	mcp := tasks.NewMCPClient(docURL+"/mcp", bearer)

	allTrials := make([]tasks.TrialResult, 0, len(corpus)*2*(*trials))
	tiers := make(map[string]int, len(corpus))
	var judgeAcc tasks.JudgeAccounting

	for _, q := range corpus {
		tiers[q.ID] = q.Tier
		for _, cfg := range []string{"A", "B"} {
			for i := 1; i <= *trials; i++ {
				res := runOneTrial(ctx, api, mcp, q, cfg, i)
				ji, jo := judgeOne(ctx, api, mcp, q, &res)
				judgeAcc.InputTokens += ji
				judgeAcc.OutputTokens += jo
				allTrials = append(allTrials, res)
				fmt.Printf("%s [%s/%d] tokens=%d correct=%v aborted=%v\n",
					q.ID, cfg, i, res.TotalTokens(), res.Correct, res.Aborted)
			}
		}
	}

	dir := newOutputDir()
	rep := report.Report{
		Metadata: report.Metadata{
			Model:      "claude-sonnet-4-6",
			GitSHA:     gitSHA(),
			CorpusHash: hashFile(*corpusPath),
			Timestamp:  time.Now().UTC(),
		},
		Trials: allTrials,
		Tiers:  tiers,
		Judge:  judgeAcc,
	}
	if err := report.WriteJSON(filepath.Join(dir, "results.json"), rep); err != nil {
		fatal("write json: %v", err)
	}
	if err := report.WriteMarkdown(filepath.Join(dir, "summary.md"), rep); err != nil {
		fatal("write md: %v", err)
	}
	fmt.Printf("tasks complete: %d trials — output: %s\n", len(allTrials), dir)
}

func runOneTrial(ctx context.Context, api tasks.API, mcp *tasks.MCPClient, q tasks.Question, cfg string, trial int) tasks.TrialResult {
	tools, handlers := buildConfig(cfg, mcp)
	res, err := tasks.RunTrial(ctx, api, handlers, tools, q.Question, tasks.RunLimits{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "trial %s/%s/%d: %v\n", q.ID, cfg, trial, err)
		res.Aborted = true
	}
	res.QuestionID = q.ID
	res.Config = cfg
	res.Trial = trial
	return res
}

func buildConfig(cfg string, mcp *tasks.MCPClient) ([]map[string]any, map[string]tasks.ToolHandler) {
	switch cfg {
	case "A":
		return tasks.ConfigATools(), map[string]tasks.ToolHandler{
			"fetch_url": func(ctx context.Context, args map[string]any) (string, error) {
				url, _ := args["url"].(string)
				return tasks.FetchURL(ctx, http.DefaultClient, url, 50_000)
			},
		}
	case "B":
		mcpHandler := func(name string) tasks.ToolHandler {
			return func(ctx context.Context, args map[string]any) (string, error) {
				return mcp.CallTool(ctx, name, args)
			}
		}
		return tasks.ConfigBTools(), map[string]tasks.ToolHandler{
			"list_sources":  mcpHandler("list_sources"),
			"search_docs":   mcpHandler("search_docs"),
			"browse_source": mcpHandler("browse_source"),
			"get_page":      mcpHandler("get_page"),
		}
	default:
		fatal("unknown config: %s", cfg)
		return nil, nil
	}
}

func judgeOne(ctx context.Context, api tasks.API, mcp *tasks.MCPClient, q tasks.Question, res *tasks.TrialResult) (int, int) {
	// Verify URL pattern matches at least one cited URL — otherwise mark incorrect
	// without spending judge tokens.
	hasMatch := false
	for _, u := range res.CitedURLs {
		if q.URLRegex().MatchString(u) {
			hasMatch = true
			break
		}
	}
	if !hasMatch && len(res.CitedURLs) > 0 {
		res.Correct = false
		res.JudgeReason = "cited URL did not match expected_url_pattern"
		return 0, 0
	}

	fetched := fetchCitedContent(ctx, mcp, res.CitedURLs)
	jr, jin, jout, err := tasks.Judge(ctx, api, tasks.JudgeInput{
		Question:         q.Question,
		Answer:           res.FinalAnswer,
		ReferenceExcerpt: q.ReferenceExcerpt,
		FetchedSources:   fetched,
	})
	if err != nil {
		res.JudgeReason = "judge error: " + err.Error()
		return jin, jout
	}
	res.Correct = jr.Correct
	res.JudgeReason = jr.Reason
	return jin, jout
}

func fetchCitedContent(ctx context.Context, mcp *tasks.MCPClient, urls []string) string {
	if len(urls) == 0 {
		return "(no URLs cited)"
	}
	var b strings.Builder
	for _, u := range urls {
		body, err := mcp.CallTool(ctx, "get_page", map[string]any{"url": u})
		if err != nil || body == "" {
			body, err = tasks.FetchURL(ctx, http.DefaultClient, u, 50_000)
			if err != nil {
				b.WriteString(fmt.Sprintf("[%s — fetch error: %v]\n", u, err))
				continue
			}
		}
		fmt.Fprintf(&b, "--- %s ---\n%s\n\n", u, body)
	}
	return b.String()
}

func fetchSourceSet(baseURL, bearer string) (map[string]bool, error) {
	names, err := fetchSources(baseURL, bearer)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

func gitSHA() string {
	out, err := os.ReadFile(".git/HEAD")
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if strings.HasPrefix(s, "ref: ") {
		ref := strings.TrimPrefix(s, "ref: ")
		body, err := os.ReadFile(filepath.Join(".git", ref))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(body))
	}
	return s
}

func hashFile(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 2: Verify build**

Run: `make bench`
Expected: build succeeds.

- [ ] **Step 3: Smoke-test with no env**

Run: `./bin/bench tasks`
Expected: stderr "bench: required env var ANTHROPIC_API_KEY not set", exit 1.

- [ ] **Step 4: Commit**

```bash
git add cmd/bench/tasks_cmd.go
git commit -m "feat(bench): wire tasks subcommand with judge and report"
```

---

## Task 15: Wire `bench all` subcommand

**Files:**
- Create: `cmd/bench/all_cmd.go`

- [ ] **Step 1: Write the implementation**

`bench all` reuses `runPageDiff` and `runTasks` but writes both into a single output directory. The cleanest path: refactor each into a function that accepts an output directory, then have `runAll` create one directory and call both. To keep the diff small, instead introduce shared `runPageDiffInto(dir)` and `runTasksInto(dir)` and have the existing entry points call them with a freshly created directory.

```go
// cmd/bench/all_cmd.go
package main

func runAll(args []string) {
	dir := newOutputDir()
	runPageDiffInto(dir, args)
	runTasksInto(dir, args)
}
```

- [ ] **Step 2: Refactor pagediff_cmd.go to expose runPageDiffInto**

In `cmd/bench/pagediff_cmd.go`, split `runPageDiff` into:

```go
func runPageDiff(args []string) {
	runPageDiffInto(newOutputDir(), args)
}

func runPageDiffInto(dir string, args []string) {
	// (existing body, but use the passed-in dir instead of calling newOutputDir())
}
```

Replace the line `dir := newOutputDir()` in the body with the parameter, and rename function appropriately.

- [ ] **Step 3: Refactor tasks_cmd.go to expose runTasksInto**

Same pattern: split `runTasks` into `runTasks(args)` (which calls `runTasksInto(newOutputDir(), args)`) and `runTasksInto(dir, args)`.

- [ ] **Step 4: Verify build and `bench all` smoke-tests**

Run: `make bench`
Run: `./bin/bench all`
Expected: stderr "bench: required env var ANTHROPIC_API_KEY not set", exit 1.

- [ ] **Step 5: Commit**

```bash
git add cmd/bench/
git commit -m "feat(bench): add `bench all` subcommand"
```

---

## Task 16: Final integration — full test pass and README note

**Files:**
- Modify: `README.md` (add a one-paragraph "Benchmarking" section)

- [ ] **Step 1: Run the full test suite**

Run: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./...`
Expected: PASS for all packages including the new `internal/bench/...`.

- [ ] **Step 2: Run vet**

Run: `CGO_ENABLED=1 go vet -tags sqlite_fts5 ./...`
Expected: no errors.

- [ ] **Step 3: Add a README section**

Find the existing top-level structure of `README.md` and add a new section near the bottom (before any "License" or trailing footnotes):

```markdown
## Benchmarking Token Savings

DocuMcp ships with a benchmark tool that measures whether using its MCP tools actually reduces agent token consumption versus a baseline of `web_search` + raw HTTP fetching. See `docs/plans/2026-04-26-token-savings-benchmark-design.md` for the full methodology.

```bash
# Build the benchmark binary
make bench

# Seed the per-page URL list from your running DocuMcp instance
./bin/bench sample-urls --per-source 5

# Run the per-page diff (no API key required for the diff itself, but token counting calls Anthropic)
ANTHROPIC_API_KEY=... ./bin/bench page-diff

# Run the full task benchmark (~few dollars in API spend at default 3 trials × 15 questions × 2 configs)
ANTHROPIC_API_KEY=... ./bin/bench tasks

# Run both into one output directory
ANTHROPIC_API_KEY=... ./bin/bench all
```

Output lands in `bench-results/<timestamp>/` (`results.json` + `summary.md`).
```

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs(readme): document the bench tool"
```

---

## Final Verification Checklist

Before declaring the plan complete:

- [ ] All packages have green tests: `CGO_ENABLED=1 go test -tags sqlite_fts5 ./...`
- [ ] `go vet -tags sqlite_fts5 ./...` is clean
- [ ] `make bench` produces `bin/bench` without warnings
- [ ] `./bin/bench --help` prints usage
- [ ] `./bin/bench page-diff` exits with a clear error when env is missing
- [ ] `./bin/bench tasks` exits with a clear error when env is missing
- [ ] Operator instructions for first run live in the README section

## Out of Scope (do not add in this plan)

- Haiku / Opus model sweep
- Auto-generated questions
- "DocuMcp + web_search" hybrid Configuration B+
- CI integration
- Latency / wall-clock measurement
- Answer-quality grading beyond binary correct/incorrect
