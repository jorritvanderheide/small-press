package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"path"

	"codeberg.org/BW20/small-press/build"
	"codeberg.org/BW20/small-press/render"
	"codeberg.org/BW20/small-press/site"
)

// The templates each kind of page is made of, in web/templates/, on top of
// base.html and posts.html.
var kinds = []string{"home", "log", "post", "thread", "work", "page", "404"}

// sections are the ids a post's page has for its own sections
// (web/templates/post.html), which a heading in its text can't have too.
var sections = site.Options{SectionIDs: []string{"references", "linked-from", "cite"}}

// page is what a template gets: the site, the page's own facts, and the
// fields of its kind.
type page struct {
	Site        *site.Site
	Path        string // where the page is, for its canonical link and the menu
	Title       string // without the site's name; "" on the home page
	Description string
	NoIndex     bool
	Build       int

	Posts      []*site.Post
	Years      []year
	Post       *site.Post
	Text       template.HTML
	References []build.Reference
	Meta       template.HTML // the citation tags and JSON-LD (build.Meta)
	Cite       template.HTML
	BibTeX     string
	Thread     *site.Thread
	Work       *site.Work
	WorkText   template.HTML
	WorkURL    string // where the work is, when its reference doesn't say
	Page       *site.Page
}

type year struct {
	Year  int
	Posts []*site.Post
}

// pages writes every page, and the static files, with the templates and
// stylesheet in assets (package web, or web/ on disk in development).
func pages(s *site.Site, assets fs.FS, o build.Options) (build.Files, error) {
	files := build.Files{}
	var errs []error
	fail := func(name string, err error) { errs = append(errs, fmt.Errorf("%s: %w", name, err)) }

	asset, err := build.Static(files, assets)
	if err != nil {
		return nil, err
	}
	funcs := build.Funcs(s)
	funcs["asset"] = asset
	templates := map[string]*template.Template{}
	for _, kind := range kinds {
		t, err := template.New(kind).Funcs(funcs).ParseFS(assets, "templates/base.html", "templates/posts.html", "templates/"+kind+".html")
		if err != nil {
			return nil, err
		}
		templates[kind] = t
	}
	write := func(name, kind string, p page) {
		p.Site, p.Build = s, o.Build
		var buf bytes.Buffer
		if err := templates[kind].ExecuteTemplate(&buf, "base", p); err != nil {
			fail(name, err)
			return
		}
		files[name] = buf.Bytes()
	}

	var live []*site.Post
	for _, p := range s.Posts {
		if p.Live() {
			live = append(live, p)
		}
	}
	write("index.html", "home", page{Path: "/", Description: s.Config.Description, Posts: live[:min(len(live), 5)]})
	write("log/index.html", "log", page{Path: "/log/", Title: "Log", Description: "Every post, newest first.", Years: byYear(live)})
	write("404.html", "404", page{Path: "/404.html", Title: "Not here", Description: s.Config.Description, NoIndex: true})

	for _, p := range s.Posts {
		name := path.Join("log", p.Slug, "index.html")
		text, err := build.PostText(s, p)
		if err != nil {
			fail(path.Join("log", p.Slug, "index.md"), err)
			continue
		}
		refs, err := build.References(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		meta, err := build.Meta(s, p, refs)
		if err != nil {
			fail(name, err)
			continue
		}
		write(name, "post", page{
			Path: p.Path(), Title: p.Title, Description: p.Summary, NoIndex: !p.Live(), Post: p, Text: text, References: refs,
			Meta: meta, Cite: build.Citation(s.Config, p), BibTeX: build.BibTeX(s.Config, p),
		})
	}

	for _, t := range s.Threads {
		description := t.Description
		if description == "" {
			description = "Posts in " + t.Name + "."
		}
		write(path.Join("threads", t.Name, "index.html"), "thread", page{Path: t.Path(), Title: t.Name, Description: description, Thread: t, Posts: t.Posts})
	}
	for _, w := range s.Works {
		html, err := render.Inline(w.Text)
		if err != nil {
			fail("works/"+w.Key+".toml", err)
			continue
		}
		write(path.Join("works", w.Key, "index.html"), "work", page{Path: w.Path(), Title: build.PlainReference(w), Description: "A work cited here, and the posts that cite it.", Work: w, WorkText: html, WorkURL: build.WorkURL(w), Posts: w.CitedBy})
	}
	for _, pg := range s.Pages {
		text, err := pg.Doc.HTML(render.Options{Link: s.Local})
		if err != nil {
			fail("pages/"+pg.Name+".md", err)
			continue
		}
		write(path.Join(pg.Name, "index.html"), "page", page{Path: pg.Path(), Title: pg.Title, Description: s.Config.Description, Page: pg, Text: text})
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return files, nil
}

// byYear groups posts, newest first, by the year they were written.
func byYear(posts []*site.Post) []year {
	var years []year
	for _, p := range posts {
		if len(years) == 0 || years[len(years)-1].Year != p.Date.Year() {
			years = append(years, year{Year: p.Date.Year()})
		}
		years[len(years)-1].Posts = append(years[len(years)-1].Posts, p)
	}
	return years
}
