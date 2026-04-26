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
