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

func TestConfigATools_HasWebSearchAndFetchURL(t *testing.T) {
	tools := ConfigATools()
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	have := map[string]bool{}
	for _, tl := range tools {
		if name, _ := tl["name"].(string); name != "" {
			have[name] = true
		}
	}
	if !have["web_search"] || !have["fetch_url"] {
		t.Errorf("missing tools: %v", have)
	}
}
