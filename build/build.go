// Package build writes everything about a site that isn't how its pages
// look: the feed, the sitemap, each post's cite.bib and images, robots.txt,
// the files nginx includes, and the static files. A site's own program writes
// its pages, with the helpers here for what every site's pages say the same
// way: a post's text, its references, how to cite it, and the tags Google
// Scholar and Zotero read. Everything is rendered into memory first, and
// written to disk only when all of it is, so an error never leaves half a
// site behind.
package build

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/BW20/small-press/dither"
	"codeberg.org/BW20/small-press/render"
	"codeberg.org/BW20/small-press/site"
)

// Files is the site: each file's path, relative to the site's root, and its
// contents.
type Files map[string][]byte

// Options are what a build takes besides the content.
type Options struct {
	// Build is the build number at the foot of every page, the commit count.
	// 0 leaves it out.
	Build int
}

// ImageWidth is the widest an image is written.
const ImageWidth = 800

// The crawlers kept out of every site, in robots.txt and with a 403.
//
//go:embed crawlers.txt
var crawlerList []byte

// Common is every file of the site but its pages and static files: each
// post's cite.bib and images, the feed, the sitemap, robots.txt, and nginx's
// includes.
func Common(s *site.Site) (Files, error) {
	files := Files{}
	var errs []error
	fail := func(name string, err error) { errs = append(errs, fmt.Errorf("%s: %w", name, err)) }

	names, err := crawlers(crawlerList)
	if err != nil {
		return nil, err
	}
	files["robots.txt"] = robots(s, names)
	files["nginx/location/crawlers.conf"] = crawlerBlock(names)
	if len(s.Aliases) > 0 {
		files["nginx/server/aliases.conf"] = aliasRedirects(s)
	}

	for _, p := range s.Posts {
		dir := path.Join("log", p.Slug)
		files[path.Join(dir, "cite.bib")] = []byte(BibTeX(s.Config, p))
		if err := writeImages(files, dir, p); err != nil {
			fail(path.Join(dir, "index.md"), err)
		}
	}

	if f, err := feed(s, func(p *site.Post) (string, error) {
		html, err := PostText(s, p)
		return string(html), err
	}); err != nil {
		fail("feed.xml", err)
	} else {
		files["feed.xml"] = f
	}
	if m, err := sitemap(s); err != nil {
		fail("sitemap.xml", err)
	} else {
		files["sitemap.xml"] = m
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return files, nil
}

// Static puts every file under static/ in assets into files, and gives the
// address of each with its hash (/static/site.css?v=1a2b3c4d), which nginx
// lets a reader keep for a year: a changed file has a new address.
func Static(files Files, assets fs.FS) (asset func(name string) (string, error), err error) {
	hashed := map[string]string{}
	err = fs.WalkDir(assets, "static", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			return err
		}
		files[name] = data
		sum := sha256.Sum256(data)
		hashed[strings.TrimPrefix(name, "static/")] = "/" + name + "?v=" + hex.EncodeToString(sum[:4])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return func(name string) (string, error) {
		if a, ok := hashed[name]; ok {
			return a, nil
		}
		return "", fmt.Errorf("no static/%s", name)
	}, nil
}

// Funcs are the template functions every site's pages may use.
func Funcs(s *site.Site) template.FuncMap {
	return template.FuncMap{
		"season": Season,
		"iso":    func(t time.Time) string { return t.Format("2006-01-02") },
		// The exact date, on a post's own page, where a reader citing it needs it.
		"date":  func(t time.Time) string { return t.Format("2 January 2006") },
		"stamp": func(t time.Time) string { return t.Format(stamp) },
		"abs":   func(p string) string { return s.Config.URL + p },
		// A menu entry is the current one on its page and the pages under
		// it: the log's on every post.
		"current": func(link, at string) bool { return link == at || link != "/" && strings.HasPrefix(at, link) },
	}
}

// Verify says whether pages has a page at every address the site gives
// something, so a link the site's checks let through leads somewhere: the
// home page, the log, each post, thread, work and page, and the 404 page.
// Two files at one name, one of the site's and one of Common's, are an
// error too.
func Verify(s *site.Site, pages, common Files) error {
	var errs []error
	for name := range pages {
		if _, ok := common[name]; ok {
			errs = append(errs, fmt.Errorf("%s: the site's pages write it, and so does the build", name))
		}
	}
	want := []string{"/", "/log/", "/404.html"}
	for _, p := range s.Posts {
		want = append(want, p.Path())
	}
	for _, t := range s.Threads {
		want = append(want, t.Path())
	}
	for _, w := range s.Works {
		want = append(want, w.Path())
	}
	for _, p := range s.Pages {
		want = append(want, p.Path())
	}
	for _, at := range want {
		name := strings.TrimPrefix(at, "/")
		if name == "" || strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		if _, ok := pages[name]; !ok {
			errs = append(errs, fmt.Errorf("%s: the site's pages have nothing at %s", name, at))
		}
	}
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errors.Join(errs...)
}

// PostText is a post's text as HTML: each image at the name it is written
// to, each citation with its work's reference as its tooltip, and each link to
// the site's own address made relative.
func PostText(s *site.Site, p *site.Post) (template.HTML, error) {
	images := imageNames(p)
	works := map[string]*site.Work{}
	for _, w := range p.Works {
		works["#ref-"+w.Key] = w
	}
	return p.Doc.HTML(render.Options{
		Image: func(dest string) string { return images[dest] },
		Link:  s.Local,
		Title: func(dest string) string {
			if w := works[dest]; w != nil {
				return PlainReference(w)
			}
			return ""
		},
	})
}

// A Reference is a work in a post's reference list.
type Reference struct {
	Key   string
	Text  template.HTML // as the style formatted it
	Plain string        // the same as plain text
	URL   string        // where the work is, when its text doesn't say (WorkURL)
}

// References are the post's references, in order.
func References(p *site.Post) ([]Reference, error) {
	var refs []Reference
	var errs []error
	for _, w := range p.Works {
		html, err := render.Inline(w.Text)
		if err != nil {
			errs = append(errs, fmt.Errorf("works/%s.toml: %w", w.Key, err))
			continue
		}
		refs = append(refs, Reference{Key: w.Key, Text: html, Plain: PlainReference(w), URL: WorkURL(w)})
	}
	return refs, errors.Join(errs...)
}

// imageNames gives each of a post's images the name it is written to: a PNG,
// as dithering makes it.
func imageNames(p *site.Post) map[string]string {
	names := map[string]string{}
	for name := range p.Images {
		names[name] = strings.TrimSuffix(name, path.Ext(name)) + ".png"
	}
	return names
}

// writeImages dithers the post's images into files, next to its page.
func writeImages(files Files, dir string, p *site.Post) error {
	names := imageNames(p)
	written := map[string]string{}
	var errs []error
	for name, out := range names {
		if other, ok := written[out]; ok {
			errs = append(errs, fmt.Errorf("images %s and %s would both be %s: rename one", other, name, out))
			continue
		}
		written[out] = name
		img, err := dither.Photo(p.Images[name], ImageWidth)
		if err != nil {
			errs = append(errs, fmt.Errorf("image %s: %w", name, err))
			continue
		}
		files[path.Join(dir, out)] = dither.PNG(img)
	}
	return errors.Join(errs...)
}

// Write puts the files in dir, which mustn't exist yet or must be empty: a
// site is written whole, never over another one.
func Write(dir string, files Files) error {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s isn't empty: the site is written to a new or empty folder", dir)
	}
	for name, data := range files {
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
