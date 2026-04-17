package githubrepo_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/documcp/documcp/internal/adapter"
	_ "github.com/documcp/documcp/internal/adapter/githubrepo"
	"github.com/documcp/documcp/internal/adapter/githubrepo"
	"github.com/documcp/documcp/internal/config"
	"github.com/documcp/documcp/internal/db"
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
