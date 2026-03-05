package web

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "iframe": true,
	"nav": true, "footer": true, "header": true, "aside": true,
}

// ExtractText parses HTML and returns the page title and visible text content.
// Title preference: first h1 > <title> tag (with site suffix stripped) > empty.
// Tags in skipTags (nav, script, style, etc.) are excluded from the output.
func ExtractText(r io.Reader) (title, content string) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", ""
	}
	var htmlTitle string
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if skipTags[tag] {
				return
			}
			if tag == "title" && htmlTitle == "" {
				htmlTitle = nodeText(n)
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
	if title == "" && htmlTitle != "" {
		// Titles often include a site name separated by " | " or " - ".
		// Keep whichever part is longer (the page title is usually longer than the site name).
		for _, sep := range []string{" | ", " - ", " – "} {
			if idx := strings.Index(htmlTitle, sep); idx > 0 {
				left := strings.TrimSpace(htmlTitle[:idx])
				right := strings.TrimSpace(htmlTitle[idx+len(sep):])
				if len(right) > len(left) {
					htmlTitle = right
				} else {
					htmlTitle = left
				}
				break
			}
		}
		title = htmlTitle
	}
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
