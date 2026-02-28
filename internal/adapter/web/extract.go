package web

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

var skipTags = map[string]bool{
	"script": true, "style": true, "nav": true,
	"footer": true, "header": true, "aside": true,
}

// ExtractText parses HTML and returns the first h1 title and visible text content.
// Tags in skipTags (nav, script, style, etc.) are excluded from the output.
func ExtractText(r io.Reader) (title, content string) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if skipTags[tag] {
				return
			}
			if tag == "h1" && title == "" {
				title = nodeText(n)
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return title, strings.TrimSpace(sb.String())
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return strings.TrimSpace(sb.String())
}
