package githubrepo_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mathwro/DocuMcp/internal/adapter"
	"github.com/mathwro/DocuMcp/internal/adapter/githubrepo"
	_ "github.com/mathwro/DocuMcp/internal/adapter/githubrepo"
	"github.com/mathwro/DocuMcp/internal/config"
	"github.com/mathwro/DocuMcp/internal/db"
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
		"README.md":        []byte("# Project\n\nIntro."),
		"docs/guide.md":    []byte("# Guide\n\nBody."),
		"docs/api/auth.md": []byte("# Auth\n\nBody."),
		"image.png":        []byte{0x89, 0x50, 0x4e, 0x47},
		"huge.md":          fiveMBPlus,
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

func TestCrawl_403_ReturnsUnauthorizedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
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

func TestCrawl_5xx_ReturnsGenericError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
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
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should include status 500: %v", err)
	}
}

func TestCrawl_BranchName_URLEscaped(t *testing.T) {
	tarball := buildTarball(t, "o-r-sha", map[string][]byte{"README.md": []byte("# R\n")})

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/x-gzip")
		_, _ = w.Write(tarball)
	}))
	t.Cleanup(srv.Close)

	_, ch, err := githubrepo.NewAdapter(srv.URL).Crawl(context.Background(), config.SourceConfig{
		Type:   "github_repo",
		Repo:   "o/r",
		Branch: "feature/new-stuff",
	}, 1)
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	_ = drainPages(context.Background(), t, ch)

	if !strings.HasSuffix(gotPath, "/repos/o/r/tarball/feature%2Fnew-stuff") {
		t.Errorf("URL path: got %q, want suffix /repos/o/r/tarball/feature%%2Fnew-stuff", gotPath)
	}
}

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
