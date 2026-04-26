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
