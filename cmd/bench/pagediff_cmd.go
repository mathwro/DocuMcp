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

type pageDiffOpts struct {
	URLsPath string
}

func runPageDiff(args []string) {
	fs := flag.NewFlagSet("page-diff", flag.ExitOnError)
	urlsPath := fs.String("urls", "internal/bench/corpus/page-urls.txt", "path to URL list")
	_ = fs.Parse(args)
	runPageDiffInto(newOutputDir(), pageDiffOpts{URLsPath: *urlsPath})
}

func runPageDiffInto(dir string, opts pageDiffOpts) {
	apiKey := mustEnv("ANTHROPIC_API_KEY")
	docURL := envOr("DOCUMCP_BENCH_URL", "http://127.0.0.1:8080")
	bearer := os.Getenv("DOCUMCP_API_KEY")

	urls, err := loadURLs(opts.URLsPath)
	if err != nil {
		fatal("load urls: %v", err)
	}
	if len(urls) == 0 {
		fatal("no URLs in %s — did you run `bench sample-urls` and commit the result?", opts.URLsPath)
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
