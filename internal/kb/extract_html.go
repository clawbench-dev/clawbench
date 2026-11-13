package kb

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
)

// extractHTML strips markup and emits one block per block-level element. It
// skips script/style/svg so code and icons do not become searchable text.
func extractHTML(path string) ([]Block, error) { //nolint:gocyclo // DOM walk with block-element switches
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open html: %w", err)
	}
	defer func() { _ = f.Close() }()

	doc, err := html.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var title string
	var findTitle func(*html.Node)
	findTitle = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findTitle(c)
		}
	}
	findTitle(doc)

	var blocks []Block
	var cur strings.Builder
	para := 0
	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s == "" {
			return
		}
		para++
		blocks = append(blocks, Block{Text: s, Location: paraRange(para, para)})
	}

	blockTags := map[string]bool{
		"p": true, "div": true, "section": true, "article": true, "header": true,
		"footer": true, "main": true, "aside": true, "li": true, "tr": true,
		"br": true, "hr": true, "pre": true, "blockquote": true, "table": true,
		"ul": true, "ol": true, "h1": true, "h2": true, "h3": true, "h4": true,
		"h5": true, "h6": true, "dd": true, "dt": true, "figcaption": true,
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "iframe", "canvas":
				return
			}
			if blockTags[n.Data] {
				flush()
			}
		}
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				if cur.Len() > 0 {
					cur.WriteString(" ")
				}
				cur.WriteString(t)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	flush()

	if title != "" {
		blocks = append([]Block{{Text: title, Location: Location{Kind: locTitle}}}, blocks...)
	}
	return blocks, nil
}
