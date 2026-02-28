package web_test

import (
	"strings"
	"testing"

	"github.com/documcp/documcp/internal/adapter/web"
)

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

func TestExtractText_NoH1(t *testing.T) {
	html := `<html><body><p>Some text</p></body></html>`
	title, content := web.ExtractText(strings.NewReader(html))
	if title != "" {
		t.Errorf("expected empty title, got %q", title)
	}
	if !strings.Contains(content, "Some text") {
		t.Error("content should be included")
	}
}
