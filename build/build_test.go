package build

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"codeberg.org/BW20/small-press/content"
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

var sections = site.Options{SectionIDs: []string{"references", "linked-from", "cite"}}

// sample is a small site: two posts up, one older than a year, one
// withdrawn, a draft, a page, a thread, a work, an alias and an image.
func sample(t *testing.T) *site.Site {
	c := &content.Site{
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
	s, err := site.New(c, sections)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func common(t *testing.T) Files {
	files, err := Common(sample(t))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func contains(t *testing.T, files Files, name string, want ...string) {
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

func lacks(t *testing.T, files Files, name string, not ...string) {
	t.Helper()
	for _, n := range not {
		if strings.Contains(string(files[name]), n) {
			t.Errorf("%s: %q in it", name, n)
		}
	}
}

func TestPostText(t *testing.T) {
	s := sample(t)
	text, err := PostText(s, post(t, s, "quiet"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h2 id="more">More</h2>`,
		`<a href="#ref-marsh2020" title="Marsh, Ann. 2020. Archives &amp; Readers.">(Marsh 2020, 12)</a>`,
		`<a href="/log/older/">before</a>`,
		`<img src="bed.png" alt="A raised bed">`,
	} {
		if !strings.Contains(string(text), want) {
			t.Errorf("no %q in\n%s", want, text)
		}
	}
}

func TestMeta(t *testing.T) {
	s := sample(t)
	p := post(t, s, "quiet")
	refs, err := References(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Text != "Marsh, Ann. 2020. <em>Archives &amp; Readers</em>." || refs[0].URL != "https://doi.org/10.1/x" {
		t.Errorf("References = %+v", refs)
	}
	meta, err := Meta(s, p, refs)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, Files{"meta": []byte(meta)}, "meta",
		`<meta name="citation_title" content="The NixOS module system is a quiet revolution">`,
		`<meta name="citation_author" content="van der Heide, Jorrit">`,
		`<meta name="citation_publication_date" content="2026/03/15">`,
		`<meta name="citation_public_url" content="https://bw20.nl/log/quiet/">`,
		`<meta name="citation_doi" content="10.64000/abcd-1234">`,
		`<meta name="citation_reference" content="Marsh, Ann. 2020. Archives &amp; Readers.">`,
		`<script type="application/ld+json">`, `"@type":"BlogPosting"`,
		`"sameAs":"https://orcid.org/0009-0005-6464-7721"`,
		`"identifier":"https://doi.org/10.64000/abcd-1234"`,
	)
	if got := string(Citation(s.Config, p)); !strings.HasPrefix(got, `van der Heide, Jorrit. 2026. “The NixOS module system is a quiet revolution.” <em>BW20</em>, 15 March. <a href="https://doi.org/10.64000/abcd-1234">`) {
		t.Errorf("Citation = %s", got)
	}
}

func TestCiteBib(t *testing.T) {
	contains(t, common(t), "log/quiet/cite.bib",
		"@online{quiet,\n",
		"  author = {van der Heide, Jorrit},\n",
		"  title = {{The NixOS module system is a quiet revolution}},\n",
		"  date = {2026-03-15},\n",
		"  doi = {10.64000/abcd-1234},\n",
		"  url = {https://bw20.nl/log/quiet/},\n",
	)
	if got := bibEscape(`50% & $5_#{x}`); got != `50\% \& \$5\_\#\{x\}` {
		t.Errorf("bibEscape = %q", got)
	}
}

// A withdrawn post keeps its cite.bib, and a draft has one, but neither is
// in the feed or the sitemap.
func TestNotListed(t *testing.T) {
	files := common(t)
	contains(t, files, "log/gone/cite.bib", "@online{gone,")
	contains(t, files, "log/soon/cite.bib", "@online{soon,")
	for _, name := range []string{"feed.xml", "sitemap.xml"} {
		lacks(t, files, name, "/log/gone/", "/log/soon/")
	}
}

func TestFeeds(t *testing.T) {
	files := common(t)
	contains(t, files, "feed.xml",
		`<feed xmlns="http://www.w3.org/2005/Atom">`,
		"<id>https://bw20.nl/log/quiet/</id>",
		`<content type="html" xml:base="https://bw20.nl/log/quiet/">`,
		"&lt;img src=&#34;bed.png&#34;",
		// As new as the newest post, not the build.
		"<updated>2026-03-15T10:00:00Z</updated>",
		"<uri>https://orcid.org/0009-0005-6464-7721</uri>",
		`<category term="tech"></category>`,
		"<rights>CC BY 4.0: https://creativecommons.org/licenses/by/4.0/</rights>",
	)
	contains(t, files, "sitemap.xml", "<loc>https://bw20.nl/log/quiet/</loc>", "<lastmod>2026-03-15</lastmod>", "<loc>https://bw20.nl/works/marsh2020/</loc>")
}

func TestServerFiles(t *testing.T) {
	files := common(t)
	contains(t, files, "robots.txt", "User-agent: *\nAllow: /\n", "User-agent: GPTBot\nDisallow: /\n", "Sitemap: https://bw20.nl/sitemap.xml")
	contains(t, files, "nginx/location/crawlers.conf", `if ($http_user_agent ~* "(GPTBot|ChatGPT-User|`, "return 403;")
	contains(t, files, "nginx/server/aliases.conf", "location = /log/nixos/ {\n    return 301 /log/quiet/;\n}\n")
	if _, err := png.DecodeConfig(bytes.NewReader(files["log/quiet/bed.png"])); err != nil {
		t.Errorf("log/quiet/bed.png: %v", err)
	}
}

func TestStatic(t *testing.T) {
	files := Files{}
	asset, err := Static(files, fstest.MapFS{
		"static/site.css":         {Data: []byte(":root {}")},
		"static/fonts/text.woff2": {Data: []byte("font")},
		"templates/base.html":     {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(files["static/fonts/text.woff2"]) != "font" || len(files) != 2 {
		t.Errorf("files = %v, want the two static files", files)
	}
	if got, err := asset("site.css"); err != nil || !strings.HasPrefix(got, "/static/site.css?v=") || len(got) != len("/static/site.css?v=")+8 {
		t.Errorf(`asset("site.css") = %q, %v`, got, err)
	}
	if _, err := asset("nope.css"); err == nil {
		t.Error("a missing file has an address")
	}
}

// A page missing at an address the site's checks let links go to is an
// error, as is a page at a name the library writes.
func TestVerify(t *testing.T) {
	s := sample(t)
	files := common(t)
	pages := Files{}
	for _, name := range []string{"index.html", "log/index.html", "404.html", "about/index.html", "threads/tech/index.html", "works/marsh2020/index.html"} {
		pages[name] = nil
	}
	for _, p := range s.Posts {
		pages["log/"+p.Slug+"/index.html"] = nil
	}
	if err := Verify(s, pages, files); err != nil {
		t.Fatal(err)
	}
	delete(pages, "works/marsh2020/index.html")
	pages["feed.xml"] = nil
	err := Verify(s, pages, files)
	if err == nil || !strings.Contains(err.Error(), "nothing at /works/marsh2020/") || !strings.Contains(err.Error(), "feed.xml: the site's pages write it") {
		t.Errorf("err = %v", err)
	}
}

func TestTime(t *testing.T) {
	for _, tt := range []struct {
		month time.Month
		want  string
	}{{time.January, "mid winter 2026"}, {time.March, "early spring 2026"}, {time.August, "late summer 2026"}, {time.December, "early winter 2026"}} {
		if got := Season(time.Date(2026, tt.month, 10, 0, 0, 0, 0, time.UTC)); got != tt.want {
			t.Errorf("Season(%v) = %q, want %q", tt.month, got, tt.want)
		}
	}
}

func TestCrawlers(t *testing.T) {
	if _, err := crawlers([]byte("GPTBot\nBad Bot\n")); err == nil {
		t.Error("a name with a space went into nginx's configuration")
	}
}

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "site")
	if err := Write(dir, Files{"log/quiet/index.html": []byte("page")}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "log", "quiet", "index.html"))
	if err != nil || string(got) != "page" {
		t.Fatalf("got %q, %v", got, err)
	}
	// Never over another site.
	if err := Write(dir, Files{"index.html": []byte("other")}); err == nil {
		t.Fatal("wrote over a site")
	}
}

// A site with no posts yet has a feed, as new as the epoch.
func TestNoPosts(t *testing.T) {
	c := &content.Site{
		Config:  content.Config{Title: "BW20", URL: "https://bw20.nl", Description: "A site.", Author: content.Author{Given: "J", Family: "H"}},
		Threads: []content.Thread{{Name: "tech", Description: "Tech."}},
	}
	s, err := site.New(c, sections)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Common(s)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, files, "feed.xml", "<updated>1970-01-01T00:00:00Z</updated>")
}

// A reference whose style gives the DOI, in either form, doesn't get it twice.
func TestWorkURL(t *testing.T) {
	doi := "https://doi.org/10.34669/WI.WJDS/5.4.3"
	for _, tt := range []struct{ text, url, want string }{
		{"Hoepman. 2025. “Is Authenticity.” https://doi.org/10.34669/WI.WJDS/5.4.3.", doi, ""},
		{"Hoepman. 2025. “Is Authenticity.” doi:10.34669/wi.wjds/5.4.3.", doi, ""},
		{"Hoepman. 2025. “Is Authenticity.”", doi, doi},
		{"A page. https://example.org/a.", "https://example.org/a", ""},
		{"No address.", "", ""},
	} {
		w := &site.Work{Work: content.Work{Text: tt.text, URL: tt.url}}
		if got := WorkURL(w); got != tt.want {
			t.Errorf("WorkURL(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// A thread only a draft names isn't announced in the sitemap.
func TestThreadFromPost(t *testing.T) {
	c := &content.Site{
		Config: content.Config{Title: "BW20", URL: "https://bw20.nl", Description: "A site.", Author: content.Author{Given: "J", Family: "H"}},
		Posts: []content.Post{
			{Slug: "open", Title: "Open", Summary: "S", Date: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), Threads: []string{"open-science"}, Body: []byte("Words.")},
			{Slug: "soon", Title: "Soon", Summary: "S", Date: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC), Threads: []string{"later"}, Draft: true, Body: []byte("Words.")},
		},
	}
	s, err := site.New(c, sections)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Common(s)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, files, "sitemap.xml", "<loc>https://bw20.nl/threads/open-science/</loc>")
	lacks(t, files, "sitemap.xml", "/threads/later/")
}

// post is the sample's post with this slug.
func post(t *testing.T, s *site.Site, slug string) *site.Post {
	for _, p := range s.Posts {
		if p.Slug == slug {
			return p
		}
	}
	t.Fatalf("no post %s", slug)
	return nil
}
