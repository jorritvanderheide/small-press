package render

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// A figure is an image and its caption: the image in a paragraph of its
// own, then a paragraph that is all italics, as In the Open writes it, or
// the italics on the line right under the image, as Obsidian shows it.
// CommonMark has no figures, so they are found here and written as a
// <figure> with a <figcaption>. An image without such italics is only an
// image: its alt text is not a caption.
var (
	kindFigure     = ast.NewNodeKind("Figure")
	kindFigcaption = ast.NewNodeKind("Figcaption")
)

type figure struct{ ast.BaseBlock }

func (n *figure) Kind() ast.NodeKind         { return kindFigure }
func (n *figure) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type figcaption struct{ ast.BaseBlock }

func (n *figcaption) Kind() ast.NodeKind         { return kindFigcaption }
func (n *figcaption) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type figureRenderer struct{}

func (figureRenderer) RegisterFuncs(r renderer.NodeRendererFuncRegisterer) {
	tag := func(open, close string) renderer.NodeRendererFunc {
		return func(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				_, _ = w.WriteString(open)
			} else {
				_, _ = w.WriteString(close)
			}
			return ast.WalkContinue, nil
		}
	}
	r.Register(kindFigure, tag("<figure>\n", "</figure>\n"))
	r.Register(kindFigcaption, tag("\n<figcaption>", "</figcaption>\n"))
}

// figures turns every image with a caption under root into a figure.
func figures(root ast.Node) {
	var paragraphs []*ast.Paragraph
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if p, ok := n.(*ast.Paragraph); ok && entering {
			paragraphs = append(paragraphs, p)
		}
		return ast.WalkContinue, nil
	})
	for _, p := range paragraphs {
		if p.Parent() == nil {
			continue // a caption already taken into a figure
		}
		img, ok := p.FirstChild().(*ast.Image)
		if !ok {
			continue
		}
		var em *ast.Emphasis
		var next *ast.Paragraph
		switch {
		case img.NextSibling() == nil:
			// The caption is the next paragraph, all italics.
			next, _ = p.NextSibling().(*ast.Paragraph)
			if next == nil {
				continue
			}
			em = only(next.FirstChild())
		default:
			// The caption is on the next line of the same paragraph.
			br, ok := img.NextSibling().(*ast.Text)
			if !ok || br.Segment.Len() > 0 || !(br.SoftLineBreak() || br.HardLineBreak()) {
				continue
			}
			em = only(br.NextSibling())
		}
		if em == nil {
			continue
		}
		fig, caption := &figure{}, &figcaption{}
		for c := em.FirstChild(); c != nil; {
			after := c.NextSibling()
			caption.AppendChild(caption, c)
			c = after
		}
		p.Parent().ReplaceChild(p.Parent(), p, fig)
		fig.AppendChild(fig, img)
		fig.AppendChild(fig, caption)
		if next != nil {
			next.Parent().RemoveChild(next.Parent(), next)
		}
	}
}

// only is n when it is italics with nothing after it, or nil.
func only(n ast.Node) *ast.Emphasis {
	em, ok := n.(*ast.Emphasis)
	if !ok || em.Level != 1 || em.NextSibling() != nil {
		return nil
	}
	return em
}
