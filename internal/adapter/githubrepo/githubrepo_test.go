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
