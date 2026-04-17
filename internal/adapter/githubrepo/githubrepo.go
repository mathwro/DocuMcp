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

// filenameTitle converts a filename like "getting-started.md" into
// "getting started" for fallback titles.
func filenameTitle(name string) string {
	n := strings.TrimSuffix(name, path.Ext(name))
	n = strings.ReplaceAll(n, "-", " ")
	n = strings.ReplaceAll(n, "_", " ")
	return n
}
