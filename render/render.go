// Package render turns Markdown into HTML: CommonMark with GFM tables,
// strikethrough and footnotes. It also reads what a text links to, for the
// checks and backlinks in package site, and says where a text breaks the
// rules a page keeps: links that are safe, images with alt text, a heading
// order that holds, and no raw HTML.
package render

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var markdown = goldmark.New(
	goldmark.WithExtensions(
		// A column's alignment as an align attribute, not a style one, which
		// the site's content security policy would block.
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough,
		extension.Footnote,
	),
	// Every heading an id made from its words, so a section can be linked to
	// and cited.
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	// An image with a caption as a <figure> (figure.go).
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(figureRenderer{}, 500))),
)

// A Doc is a Markdown text, parsed.
type Doc struct {
	src  []byte
	root ast.Node
}

// Parse parses src.
func Parse(src []byte) *Doc {
	return &Doc{src: src, root: markdown.Parser().Parse(text.NewReader(src))}
}

// A Link is a link or an image in a text.
type Link struct {
	Dest  string
	Words string // a link's words, an image's alt text
	Image bool
}

// Links lists the text's links and images, in order.
func (d *Doc) Links() []Link {
	var links []Link
	ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link:
			links = append(links, Link{Dest: string(n.Destination), Words: words(n, d.src)})
		case *ast.AutoLink:
			links = append(links, Link{Dest: string(n.URL(d.src)), Words: string(n.Label(d.src))})
		case *ast.Image:
			links = append(links, Link{Dest: string(n.Destination), Words: words(n, d.src), Image: true})
		}
		return ast.WalkContinue, nil
	})
	return links
}

// Problems says where the text breaks the rules a page keeps, each as a
// sentence.
func (d *Doc) Problems() []string {
	var problems []string
	level := 1 // the page's own <h1>
	ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			switch {
			case n.Level == 1:
				problems = append(problems, fmt.Sprintf("heading %q is a level 1 heading: the page's title is its only one, so a text's headings start at ##", words(n, d.src)))
			case n.Level > level+1:
				problems = append(problems, fmt.Sprintf("heading %q skips a level: after a level %d heading comes one of level %d at most", words(n, d.src), level, level+1))
			}
			level = n.Level
		case *ast.HTMLBlock, *ast.RawHTML:
			problems = append(problems, "raw HTML is left out of the page: write it in Markdown")
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	for _, l := range d.Links() {
		switch {
		case l.Image && l.Words == "":
			problems = append(problems, fmt.Sprintf("image %s has no alt text: ![what it shows](%s)", l.Dest, l.Dest))
		case l.Image && (strings.ContainsAny(l.Dest, "/:?#") || l.Dest == ""):
			problems = append(problems, fmt.Sprintf("image %s isn't a file next to the text: an image is ![alt](name.png)", l.Dest))
		case !l.Image && !Safe(l.Dest):
			problems = append(problems, fmt.Sprintf("link %q goes to %s: a link is http, https, mailto, or a path on the site starting with /", l.Words, l.Dest))
		}
	}
	return problems
}

// Safe says whether dest is a link a page may have: http, https, mailto,
// a path on the site, or a place on the page.
func Safe(dest string) bool {
	if strings.HasPrefix(dest, "#") || (strings.HasPrefix(dest, "/") && !strings.HasPrefix(dest, "//")) {
		return true
	}
	u, err := url.Parse(dest)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != ""
	}
	return false
}

// Options change links and images as a text is rendered.
type Options struct {
	// Image gives the address an image's file is written to.
	Image func(dest string) string
	// Title gives a link's title, its tooltip, or "" for none.
	Title func(dest string) string
	// Link gives the address a link is written with, such as a site's own
	// address made relative.
	Link func(dest string) string
}

// HTML renders the text.
func (d *Doc) HTML(o Options) (template.HTML, error) {
	// A parse of its own, as the options change the tree.
	root := markdown.Parser().Parse(text.NewReader(d.src))
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			if o.Image != nil {
				n.Destination = []byte(o.Image(string(n.Destination)))
			}
		case *ast.Link:
			if o.Title != nil {
				if t := o.Title(string(n.Destination)); t != "" {
					n.Title = []byte(t)
				}
			}
			if o.Link != nil {
				n.Destination = []byte(o.Link(string(n.Destination)))
			}
		}
		return ast.WalkContinue, nil
	})
	figures(root)
	var buf bytes.Buffer
	if err := markdown.Renderer().Render(&buf, d.src, root); err != nil {
		return "", err
	}
	// goldmark escapes the text and leaves raw HTML out, so the result is
	// safe to put in a page as it is.
	return template.HTML(buf.String()), nil
}

// Inline renders one line of Markdown, such as a work's reference, without
// the paragraph around it.
func Inline(src string) (template.HTML, error) {
	html, err := Parse([]byte(src)).HTML(Options{})
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(html))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "<p>"), "</p>")
	return template.HTML(s), nil
}

// words is the text inside n, without its markup.
func words(n ast.Node, src []byte) string {
	var b strings.Builder
	ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch c := c.(type) {
			case *ast.Text:
				b.Write(c.Segment.Value(src))
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(c.Value)
			case *ast.AutoLink:
				// Its address is its words, and it has no Text inside.
				b.Write(c.Label(src))
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// Plain is one line of Markdown as plain text, for a tooltip or a meta tag.
func Plain(src string) string {
	d := Parse([]byte(src))
	return strings.TrimSpace(words(d.root, d.src))
}

// A Heading is a heading in a text: its words, the id it is written with,
// and its level, 2 for ##, as a site's table of contents needs.
type Heading struct {
	Words string
	ID    string
	Level int
}

// Headings lists the text's headings, in order.
func (d *Doc) Headings() []Heading {
	var headings []Heading
	ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			id, _ := h.AttributeString("id")
			value, _ := id.([]byte)
			headings = append(headings, Heading{Words: words(h, d.src), ID: string(value), Level: h.Level})
		}
		return ast.WalkContinue, nil
	})
	return headings
}
