// Package site is the site as a whole: posts newest first, their threads,
// the works they cite, which posts link to which, and every check that needs
// more than one file. It reads only what package content read, so it is
// tested on an in-memory site.
package site

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"codeberg.org/BW20/small-press/content"
	"codeberg.org/BW20/small-press/render"
)

// Site is everything content/ holds, put together and checked.
type Site struct {
	Config  content.Config
	Posts   []*Post   // newest first, withdrawn ones too
	Pages   []*Page   // by name
	Threads []*Thread // as threads.toml lists them
	Works   []*Work   // by key: the works some post cites
	Aliases map[string]string
}

// A Post is a post with its text parsed, and what the rest of the site says
// about it.
type Post struct {
	content.Post
	Doc        *render.Doc
	ThreadList []*Thread
	Works      []*Work // its references, in order
	LinkedFrom []*Post // live posts that link to it, newest first
}

// Live says whether the post is up: published, and not withdrawn. Only a
// live post is in the site's lists, its feed and its sitemap.
func (p *Post) Live() bool { return p.Withdrawn.IsZero() && !p.Draft }

// Path is where the post is on the site.
func (p *Post) Path() string { return "/log/" + p.Slug + "/" }

// A Page is a page with its text parsed.
type Page struct {
	content.Page
	Doc *render.Doc
}

// Path is where the page is on the site.
func (p *Page) Path() string { return "/" + p.Name + "/" }

// A Thread is a thread with its live posts, newest first.
type Thread struct {
	content.Thread
	Posts  []*Post
	Listed bool // in threads.toml, which keeps its page when no post is in it
}

// Path is where the thread is on the site.
func (t *Thread) Path() string { return "/threads/" + t.Name + "/" }

// A Work is a work with the live posts that cite it, newest first.
type Work struct {
	content.Work
	CitedBy []*Post
}

// Path is where the work is on the site.
func (w *Work) Path() string { return "/works/" + w.Key + "/" }

// Files at the site's root, which a link may go to.
var rootFiles = []string{"/feed.xml", "/sitemap.xml", "/robots.txt"}

// Names a page can't have: the site's own folders are there.
var reserved = []string{"log", "threads", "works", "static", "nginx"}

// Options are what a site's own program tells New about its pages.
type Options struct {
	// SectionIDs are the ids a post's page has for its own sections, such as
	// references, which a heading in its text can't have too.
	SectionIDs []string
}

// New puts c together, and checks it: everything wrong is in the error,
// each by its file.
func New(c *content.Site, o Options) (*Site, error) {
	s := &Site{Config: c.Config, Aliases: map[string]string{}}
	var errs []error
	fail := func(file, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", file, fmt.Sprintf(format, args...)))
	}

	// Every thread a post names, and threads.toml's, described there or not.
	listed := map[string]content.Thread{}
	for _, t := range c.Threads {
		listed[t.Name] = t
	}
	threads := map[string]*Thread{}
	for _, name := range c.ThreadNames() {
		t, ok := listed[name]
		if !ok {
			t = content.Thread{Name: name}
		}
		th := &Thread{Thread: t, Listed: ok}
		s.Threads = append(s.Threads, th)
		threads[name] = th
	}
	works := map[string]*Work{}
	for _, w := range c.Works {
		works[w.Key] = &Work{Work: w}
	}
	for _, p := range c.Pages {
		page := &Page{Page: p, Doc: render.Parse(p.Body)}
		file := "pages/" + p.Name + ".md"
		if slices.Contains(reserved, p.Name) {
			fail(file, "a page can't be called %s: the site's /%s/ is there", p.Name, p.Name)
		}
		for _, problem := range page.Doc.Problems() {
			fail(file, "%s", problem)
		}
		s.Pages = append(s.Pages, page)
	}

	posts := map[string]*Post{}
	for _, p := range c.Posts {
		post := &Post{Post: p, Doc: render.Parse(p.Body)}
		s.Posts = append(s.Posts, post)
		posts[p.Slug] = post
	}
	// Newest first; by slug when two have the same date, so a build is the
	// same every time.
	slices.SortFunc(s.Posts, func(a, b *Post) int {
		if c := b.Date.Compare(a.Date); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})

	for _, p := range s.Posts {
		file := "log/" + p.Slug + "/index.md"
		for _, a := range p.Aliases {
			switch {
			case !content.IsName(a):
				fail(file, "alias %s isn't a slug: lowercase letters, digits and hyphens", a)
			case posts[a] != nil:
				fail(file, "alias %s is another post's slug", a)
			case s.Aliases[a] != "":
				fail(file, "alias %s is already log/%s's", a, s.Aliases[a])
			default:
				s.Aliases[a] = p.Slug
			}
		}
		for _, name := range p.Threads {
			t := threads[name]
			if !content.IsName(name) {
				fail(file, "thread %q isn't a name: lowercase letters, digits and hyphens, as tech or open-science", name)
				continue
			}
			p.ThreadList = append(p.ThreadList, t)
			if p.Live() {
				t.Posts = append(t.Posts, p)
			}
		}
		for i, key := range p.References {
			w := works[key]
			switch {
			case slices.Contains(p.References[:i], key):
				fail(file, "reference %s is listed twice", key)
			case w == nil:
				fail(file, "reference %s has no work: works/%s.toml", key, key)
			default:
				p.Works = append(p.Works, w)
				if p.Live() {
					w.CitedBy = append(w.CitedBy, p)
				}
			}
		}
	}

	// What a link on the site may go to.
	paths := map[string]bool{"/": true, "/log/": true}
	for _, f := range rootFiles {
		paths[f] = true
	}
	for _, p := range s.Posts {
		paths[p.Path()] = true
		paths[p.Path()+"cite.bib"] = true
	}
	for _, p := range s.Pages {
		paths[p.Path()] = true
	}
	for _, t := range s.Threads {
		paths[t.Path()] = true
	}
	for _, key := range sortedKeys(works) {
		// Only works a post cites have a page; a withdrawn post's keep theirs.
		w := works[key]
		for _, p := range s.Posts {
			if slices.Contains(p.References, key) {
				s.Works = append(s.Works, w)
				paths[w.Path()] = true
				break
			}
		}
	}

	for _, p := range s.Posts {
		file := "log/" + p.Slug + "/index.md"
		for _, problem := range p.Doc.Problems() {
			fail(file, "%s", problem)
		}
		for _, h := range p.Doc.Headings() {
			if slices.Contains(o.SectionIDs, h.ID) {
				fail(file, "heading %q would have the id %s, which the page has for its own section: call it something else", h.Words, h.ID)
			}
		}
		cited := map[string]bool{}
		used := map[string]bool{}
		for _, l := range p.Doc.Links() {
			if l.Image {
				// An address that isn't a file name is in the problems already.
				if _, ok := p.Images[l.Dest]; !ok && !strings.ContainsAny(l.Dest, "/:?#") {
					fail(file, "image %s isn't in log/%s/", l.Dest, p.Slug)
				}
				used[l.Dest] = true
				continue
			}
			dest := s.Local(l.Dest)
			if key, ok := strings.CutPrefix(dest, "#ref-"); ok {
				if !slices.Contains(p.References, key) {
					fail(file, "citation %q of %s has no reference: add %s to references", l.Words, key, key)
				}
				cited[key] = true
				continue
			}
			if !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
				continue // outside the site, or on the page itself
			}
			u, err := url.Parse(dest)
			if err != nil {
				fail(file, "link %q goes to %s, which isn't an address", l.Words, l.Dest)
				continue
			}
			if slug, ok := strings.CutPrefix(u.Path, "/log/"); ok {
				slug = strings.TrimSuffix(slug, "/")
				if now := s.Aliases[slug]; now != "" {
					fail(file, "link %q goes to %s, an old slug of log/%s: link to /log/%s/", l.Words, l.Dest, now, now)
					continue
				}
				if to := posts[slug]; to != nil && to.Draft && !p.Draft {
					fail(file, "link %q goes to %s, which is a draft: publish it first", l.Words, l.Dest)
					continue
				}
				if to := posts[slug]; to != nil && to != p && p.Live() && !slices.Contains(to.LinkedFrom, p) {
					to.LinkedFrom = append(to.LinkedFrom, p)
				}
			}
			if !paths[u.Path] {
				fail(file, "link %q goes to %s, which the site doesn't have", l.Words, l.Dest)
			}
		}
		for _, key := range p.References {
			if !cited[key] {
				fail(file, "reference %s is never cited: cite it as [(Author Year)](#ref-%s), or take it out of references", key, key)
			}
		}
		for name := range p.Images {
			if !used[name] {
				fail(file, "image %s is next to the post, but not in it", name)
			}
		}
	}

	// A page has no references, so it cites nothing; it may link to a work's page.
	for _, p := range s.Pages {
		for _, l := range p.Doc.Links() {
			if l.Image {
				fail("pages/"+p.Name+".md", "image %s: a page has no images, only a post has", l.Dest)
				continue
			}
			dest := s.Local(l.Dest)
			if !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
				continue
			}
			u, err := url.Parse(dest)
			if err != nil || !paths[u.Path] {
				fail("pages/"+p.Name+".md", "link %q goes to %s, which the site doesn't have", l.Words, l.Dest)
				continue
			}
			if slug, ok := strings.CutPrefix(u.Path, "/log/"); ok {
				if to := posts[strings.TrimSuffix(slug, "/")]; to != nil && to.Draft {
					fail("pages/"+p.Name+".md", "link %q goes to %s, which is a draft: publish it first", l.Words, l.Dest)
				}
			}
		}
	}

	for _, m := range s.Config.Menu {
		if !paths[m.Link] {
			fail("site.toml", "menu entry %s goes to %s, which the site doesn't have", m.Name, m.Link)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Local is dest as the site writes it: a link to the site's own address,
// https://bw20.nl/log/<slug>/, as /log/<slug>/, and any other as it is.
func (s *Site) Local(dest string) string {
	if rest, ok := strings.CutPrefix(dest, s.Config.URL); ok && strings.HasPrefix(rest, "/") {
		return rest
	}
	return dest
}
