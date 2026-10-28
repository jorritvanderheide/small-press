package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"maps"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"codeberg.org/BW20/small-press/build"
	"codeberg.org/BW20/small-press/content"
	"codeberg.org/BW20/small-press/example/web"
	"codeberg.org/BW20/small-press/site"
)

func photo(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	for y := range 600 {
		for x := range 1200 {
			img.Set(x, y, color.RGBA{uint8(x / 5), 120, 80, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// sample is a small site: two posts up, one older than a year, one
// withdrawn, a draft, a page, a thread, a work, an alias and an image.
func sample(t *testing.T) *content.Site {
	return &content.Site{
		Config: content.Config{
			Title: "BW20", URL: "https://bw20.nl", Description: "A small, finite website.",
			Author: content.Author{Given: "Jorrit", Family: "van der Heide", ORCID: "0009-0005-6464-7721"},
			Menu:   []content.MenuItem{{Name: "log", Link: "/log/"}, {Name: "about", Link: "/about/"}},
		},
		Threads: []content.Thread{{Name: "tech", Description: "Notes on NixOS."}},
		Works:   []content.Work{{Key: "marsh2020", Text: "Marsh, Ann. 2020. *Archives & Readers*.", URL: "https://doi.org/10.1/x"}},
		Pages:   []content.Page{{Name: "about", Title: "About", Body: []byte("Who, why.")}},
		Posts: []content.Post{
			{Slug: "quiet", Title: "The NixOS module system is a quiet revolution", Summary: "Why it matters.",
				Date: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC), Threads: []string{"tech"}, Aliases: []string{"nixos"},
				DOI:        "10.64000/abcd-1234",
				References: []string{"marsh2020"},
				Body:       []byte("As [(Marsh 2020, 12)](#ref-marsh2020) says, and [before](https://bw20.nl/log/older/).\n\n![A raised bed](bed.jpg)\n\n## More"),
				Images:     map[string][]byte{"bed.jpg": photo(t)}},
			{Slug: "older", Title: "Older", Summary: "From before.", Date: time.Date(2025, 1, 5, 10, 0, 0, 0, time.UTC),
				Body: []byte("Old words.")},
			{Slug: "gone", Title: "Gone", Summary: "Not any more.", Date: time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
				Withdrawn: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC), WithdrawnReason: "The figures were wrong.", Body: []byte("Its words.")},
			{Slug: "soon", Title: "Soon", Summary: "Not yet.", Date: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), Threads: []string{"tech"},
				Draft: true, References: []string{"marsh2020"}, Body: []byte("Draft words, as [(Marsh 2020)](#ref-marsh2020) says.")},
		},
	}
}

// whole is the whole site from c, as the program builds it: the pages and
// the library's files, checked against each other.
func whole(t *testing.T, c *content.Site, o build.Options) build.Files {
	s, err := site.New(c, sections)
	if err != nil {
		t.Fatal(err)
	}
	files, err := pages(s, web.Files, o)
	if err != nil {
		t.Fatal(err)
	}
	common, err := build.Common(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := build.Verify(s, files, common); err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, common)
	return files
}

func contains(t *testing.T, files build.Files, name string, want ...string) {
	t.Helper()
	f, ok := files[name]
	if !ok {
		t.Errorf("no %s", name)
		return
	}
	for _, w := range want {
		if !strings.Contains(string(f), w) {
			t.Errorf("%s: no %q in\n%s", name, w, f)
		}
	}
}

func lacks(t *testing.T, files build.Files, name string, not ...string) {
	t.Helper()
	for _, n := range not {
		if strings.Contains(string(files[name]), n) {
			t.Errorf("%s: %q in it", name, n)
		}
	}
}

func TestPost(t *testing.T) {
	files := whole(t, sample(t), build.Options{Build: 42})
	contains(t, files, "log/quiet/index.html",
		"<title>The NixOS module system is a quiet revolution · BW20</title>",
		`<link rel="canonical" href="https://bw20.nl/log/quiet/">`,
		`<link rel="stylesheet" href="/static/site.css?v=`,
		`<a href="/log/" aria-current="page">log</a>`,
		`<time datetime="2026-03-15">15 March 2026</time>`,
		`<meta name="citation_doi" content="10.64000/abcd-1234">`,
		`<a href="https://doi.org/10.64000/abcd-1234">https://doi.org/10.64000/abcd-1234</a>`,
		`<link rel="license" href="https://creativecommons.org/licenses/by/4.0/">`,
		`<a rel="author" href="https://orcid.org/0009-0005-6464-7721">Jorrit van der Heide</a>`,
		`<h2 id="more">More</h2>`,
		`in <a href="/threads/tech/">tech</a>`,
		`<a class="cited" href="/works/marsh2020/">Where it’s cited</a>`,
		`<li id="ref-marsh2020">Marsh, Ann. 2020. <em>Archives &amp; Readers</em>. <a href="https://doi.org/10.1/x">`,
		`van der Heide, Jorrit. 2026. “The NixOS module system is a quiet revolution.” <em>BW20</em>, 15 March.`,
		"@online{quiet,",
		`<meta property="og:type" content="article">`,
		`<meta name="citation_title" content="The NixOS module system is a quiet revolution">`,
		`<script type="application/ld+json">`,
		"Build 42.",
	)
	// Linked from the newer post.
	contains(t, files, "log/older/index.html", "Linked from", `<a href="/log/quiet/">`)
	lacks(t, files, "log/quiet/index.html", "Linked from")
	contains(t, files, "static/site.css", ":root")
}

func TestWithdrawn(t *testing.T) {
	files := whole(t, sample(t), build.Options{})
	// Still readable, as a retracted article is, and saying so before its text.
	contains(t, files, "log/gone/index.html", `<meta name="robots" content="noindex">`,
		`<p class="notice"><strong>Withdrawn on <time datetime="2026-04-01">1 April 2026</time>.</strong> The figures were wrong.<br>It’s`, "Its words.")
	lacks(t, files, "log/gone/index.html", `rel="canonical"`)
	// Off every list.
	for _, name := range []string{"index.html", "log/index.html", "threads/tech/index.html"} {
		lacks(t, files, name, "/log/gone/")
	}
}

// A draft is a whole post at its address, and nowhere else.
func TestDraft(t *testing.T) {
	files := whole(t, sample(t), build.Options{})
	contains(t, files, "log/soon/index.html", `<meta name="robots" content="noindex">`, "<strong>A draft.</strong>", "Draft words", `<li id="ref-marsh2020">`)
	lacks(t, files, "log/soon/index.html", `rel="canonical"`)
	for _, name := range []string{"index.html", "log/index.html", "threads/tech/index.html", "works/marsh2020/index.html"} {
		lacks(t, files, name, "/log/soon/")
	}
	lacks(t, files, "log/quiet/index.html", "A draft.")
}

func TestLists(t *testing.T) {
	files := whole(t, sample(t), build.Options{})
	contains(t, files, "index.html", "<title>BW20</title>", "A small, finite website.", `<a href="/log/quiet/">`, `<a class="name" href="/" aria-current="page">`)
	contains(t, files, "log/index.html", "<h2>2026</h2>", "<h2>2025</h2>", `<time datetime="2026-03-15">early spring 2026</time>`)
	contains(t, files, "threads/tech/index.html", "<h1>tech</h1>", "Notes on NixOS.", `<a href="/log/quiet/">`)
	contains(t, files, "works/marsh2020/index.html", "<em>Archives &amp; Readers</em>", "Cited in", `<a href="/log/quiet/">`)
	contains(t, files, "about/index.html", "<h1>About</h1>", "<p>Who, why.</p>", `<a href="/about/" aria-current="page">about</a>`)
	contains(t, files, "404.html", `<meta name="robots" content="noindex">`, "Not here")
}

// A template that fails makes no site at all, not one without that page.
func TestTemplateError(t *testing.T) {
	assets := fstest.MapFS{"static/site.css": {}}
	for _, kind := range kinds {
		assets["templates/"+kind+".html"] = &fstest.MapFile{Data: []byte(`{{define "main"}}{{end}}`)}
	}
	assets["templates/posts.html"] = &fstest.MapFile{}
	assets["templates/base.html"] = &fstest.MapFile{Data: []byte(`{{define "base"}}{{.Missing}}{{end}}`)}
	s, err := site.New(sample(t), sections)
	if err != nil {
		t.Fatal(err)
	}
	files, err := pages(s, assets, build.Options{})
	if err == nil || !strings.Contains(err.Error(), "log/quiet/index.html") {
		t.Fatalf("err = %v, want one naming log/quiet/index.html", err)
	}
	if files != nil {
		t.Errorf("files = %v, want none", files)
	}
}

// A site with no posts yet builds, and says so.
func TestNoPosts(t *testing.T) {
	c := &content.Site{
		Config:  content.Config{Title: "BW20", URL: "https://bw20.nl", Description: "A site.", Author: content.Author{Given: "J", Family: "H"}},
		Threads: []content.Thread{{Name: "tech", Description: "Tech."}},
	}
	files := whole(t, c, build.Options{})
	contains(t, files, "index.html", "No posts yet.")
	contains(t, files, "log/index.html", "No posts yet.")
	contains(t, files, "threads/tech/index.html", "No posts in this thread yet.")
	lacks(t, files, "index.html", "Newest")
}

// A thread only a post names gets a page, as does one only a draft names.
func TestThreadFromPost(t *testing.T) {
	c := &content.Site{
		Config: content.Config{Title: "BW20", URL: "https://bw20.nl", Description: "A site.", Author: content.Author{Given: "J", Family: "H"}},
		Posts: []content.Post{
			{Slug: "open", Title: "Open", Summary: "S", Date: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), Threads: []string{"open-science"}, Body: []byte("Words.")},
			{Slug: "soon", Title: "Soon", Summary: "S", Date: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), Threads: []string{"later"}, Draft: true, Body: []byte("Words.")},
		},
	}
	files := whole(t, c, build.Options{})
	contains(t, files, "threads/open-science/index.html", "<h1>open-science</h1>", `content="Posts in open-science."`, `<a href="/log/open/">`)
	lacks(t, files, "threads/open-science/index.html", `<p class="lead">`)
	contains(t, files, "threads/later/index.html", "No posts in this thread yet.")
}
