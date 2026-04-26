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

	docFetch := func(_ context.Context, _ string) (string, error) {
		return "doc", nil
	}
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
	if r.TokensDocuMcp != 3 {
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
