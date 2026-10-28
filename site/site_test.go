package site

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"codeberg.org/BW20/small-press/content"
)

func day(d int) time.Time { return time.Date(2026, 3, d, 10, 0, 0, 0, time.UTC) }

// sample is a whole site, as it should be; a test changes what it tests.
func sample() *content.Site {
	return &content.Site{
		Config: content.Config{
			Title: "BW20", URL: "https://bw20.nl", Description: "A site.",
			Author: content.Author{Given: "Jorrit", Family: "van der Heide"},
			Menu:   []content.MenuItem{{Name: "log", Link: "/log/"}, {Name: "about", Link: "/about/"}},
		},
		Threads: []content.Thread{{Name: "tech", Description: "Tech."}, {Name: "garden", Description: "Garden."}},
		Works:   []content.Work{{Key: "marsh2020", Text: "Marsh. 2020."}, {Key: "unused", Text: "Nobody."}},
		Pages:   []content.Page{{Name: "about", Title: "About", Body: []byte("See [the newest](/log/newer/).")}},
		Posts: []content.Post{
			{Slug: "older", Title: "Older", Summary: "S", Date: day(1), Threads: []string{"tech"},
				Aliases: []string{"old"}, Body: []byte("Nothing yet.")},
			{Slug: "newer", Title: "Newer", Summary: "S", Date: day(9), Threads: []string{"tech", "garden"},
				References: []string{"marsh2020"},
				Body:       []byte("As [(Marsh 2020)](#ref-marsh2020) says, see [before](https://bw20.nl/log/older/#a) and ![a bed](bed.png)."),
				Images:     map[string][]byte{"bed.png": nil}},
			{Slug: "gone", Title: "Gone", Summary: "S", Date: day(5), Threads: []string{"tech"}, Withdrawn: day(6),
				Body: []byte("Linked [before](/log/older/).")},
		},
	}
}

func TestNew(t *testing.T) {
	s, err := New(sample(), testOptions)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, p := range s.Posts {
		slugs = append(slugs, p.Slug)
	}
	if got := strings.Join(slugs, " "); got != "newer gone older" {
		t.Errorf("posts = %s, want newest first", got)
	}
	newer, gone, older := s.Posts[0], s.Posts[1], s.Posts[2]

	// Linked from: live posts only, not the withdrawn one.
	if len(older.LinkedFrom) != 1 || older.LinkedFrom[0] != newer {
		t.Errorf("older.LinkedFrom = %v, want newer", older.LinkedFrom)
	}
	if gone.Live() || !newer.Live() {
		t.Error("Live is wrong")
	}
	// Threads keep their live posts, newest first.
	if tech := s.Threads[0]; len(tech.Posts) != 2 || tech.Posts[0] != newer || tech.Posts[1] != older {
		t.Errorf("tech.Posts = %v", tech.Posts)
	}
	if len(newer.ThreadList) != 2 {
		t.Errorf("newer.ThreadList = %v", newer.ThreadList)
	}
	// Only works a post cites have a page.
	if len(s.Works) != 1 || s.Works[0].Key != "marsh2020" || len(s.Works[0].CitedBy) != 1 {
		t.Errorf("Works = %+v", s.Works)
	}
	if len(newer.Works) != 1 || newer.Works[0] != s.Works[0] {
		t.Errorf("newer.Works = %v", newer.Works)
	}
	if s.Aliases["old"] != "older" {
		t.Errorf("Aliases = %v", s.Aliases)
	}
}

// Each rule of "a broken site does not build", by the file it's in.
func TestNewErrors(t *testing.T) {
	tests := []struct {
		name   string
		change func(*content.Site)
		want   string
	}{
		{"link to a post that doesn't exist", func(c *content.Site) {
			c.Posts[0].Body = []byte("[x](/log/nope/)")
		}, `log/older/index.md: link "x" goes to /log/nope/, which the site doesn't have`},
		{"link to an alias", func(c *content.Site) {
			c.Posts[1].Body = []byte("[x](/log/old/) [(Marsh 2020)](#ref-marsh2020) ![a bed](bed.png)")
		}, "an old slug of log/older: link to /log/older/"},
		{"citation without a reference", func(c *content.Site) {
			c.Posts[0].Body = []byte("[(Marsh 2020)](#ref-marsh2020)")
		}, `log/older/index.md: citation "(Marsh 2020)" of marsh2020 has no reference`},
		{"reference never cited", func(c *content.Site) {
			c.Posts[0].References = []string{"marsh2020"}
		}, "log/older/index.md: reference marsh2020 is never cited"},
		{"reference without a work", func(c *content.Site) {
			c.Posts[1].References = []string{"marsh2020", "lee2021"}
		}, "log/newer/index.md: reference lee2021 has no work: works/lee2021.toml"},
		{"reference twice", func(c *content.Site) {
			c.Posts[1].References = []string{"marsh2020", "marsh2020"}
		}, "reference marsh2020 is listed twice"},
		{"a thread that isn't a name", func(c *content.Site) {
			c.Posts[0].Threads = []string{"Cooking"}
		}, `log/older/index.md: thread "Cooking" isn't a name`},
		{"alias that is a slug", func(c *content.Site) {
			c.Posts[0].Aliases = []string{"newer"}
		}, "log/older/index.md: alias newer is another post's slug"},
		{"alias twice", func(c *content.Site) {
			c.Posts[1].Aliases = []string{"old"}
		}, "log/older/index.md: alias old is already log/newer's"},
		{"image that isn't there", func(c *content.Site) {
			c.Posts[1].Images = map[string][]byte{}
		}, "log/newer/index.md: image bed.png isn't in log/newer/"},
		{"image that isn't used", func(c *content.Site) {
			c.Posts[0].Images = map[string][]byte{"spare.png": nil}
		}, "log/older/index.md: image spare.png is next to the post, but not in it"},
		{"a text's problems", func(c *content.Site) {
			c.Posts[0].Body = []byte("# Again")
		}, "log/older/index.md: heading \"Again\" is a level 1 heading"},
		{"a heading with a section's id", func(c *content.Site) {
			c.Posts[0].Body = []byte("## References")
		}, `log/older/index.md: heading "References" would have the id references`},
		{"a page's problems", func(c *content.Site) {
			c.Pages[0].Body = []byte("<b>x</b>")
		}, "pages/about.md: raw HTML"},
		{"an absolute link to a post the site doesn't have", func(c *content.Site) {
			c.Posts[0].Body = []byte("[x](https://bw20.nl/log/nope/)")
		}, `log/older/index.md: link "x" goes to https://bw20.nl/log/nope/, which the site doesn't have`},
		{"a page's broken link", func(c *content.Site) {
			c.Pages[0].Body = []byte("[x](/nope/)")
		}, `pages/about.md: link "x" goes to /nope/`},
		{"an alias that isn't a slug", func(c *content.Site) {
			c.Posts[0].Aliases = []string{"Old One"}
		}, "log/older/index.md: alias Old One isn't a slug"},
		{"an image on a page", func(c *content.Site) {
			c.Pages[0].Body = []byte("![a bed](bed.png)")
		}, "pages/about.md: image bed.png: a page has no images"},
		{"a reserved page name", func(c *content.Site) {
			c.Pages = append(c.Pages, content.Page{Name: "works", Title: "W"})
		}, "pages/works.md: a page can't be called works"},
		{"link to a draft", func(c *content.Site) {
			c.Posts = append(c.Posts, content.Post{Slug: "soon", Title: "Soon", Summary: "S", Date: day(10), Draft: true})
			c.Posts[0].Body = []byte("[x](/log/soon/)")
		}, `log/older/index.md: link "x" goes to /log/soon/, which is a draft: publish it first`},
		{"a page's link to a draft", func(c *content.Site) {
			c.Posts = append(c.Posts, content.Post{Slug: "soon", Title: "Soon", Summary: "S", Date: day(10), Draft: true})
			c.Pages[0].Body = []byte("[x](https://bw20.nl/log/soon/)")
		}, `pages/about.md: link "x" goes to https://bw20.nl/log/soon/, which is a draft`},
		{"a menu entry to nowhere", func(c *content.Site) {
			c.Config.Menu = append(c.Config.Menu, content.MenuItem{Name: "now", Link: "/now/"})
		}, "site.toml: menu entry now goes to /now/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := sample()
			tt.change(c)
			_, err := New(c, testOptions)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one saying %q", err, tt.want)
			}
		})
	}
}

// A draft is in no list, links from it count for nothing, and it may link
// to another draft.
func TestDraft(t *testing.T) {
	c := sample()
	c.Posts = append(c.Posts,
		content.Post{Slug: "soon", Title: "Soon", Summary: "S", Date: day(10), Threads: []string{"tech"}, Draft: true,
			References: []string{"marsh2020"}, Body: []byte("As [(Marsh 2020)](#ref-marsh2020) says, [before](/log/older/) and [later](/log/later/).")},
		content.Post{Slug: "later", Title: "Later", Summary: "S", Date: day(11), Draft: true, Body: []byte("Not yet.")},
	)
	s, err := New(c, testOptions)
	if err != nil {
		t.Fatal(err)
	}
	later, soon, newer, older := s.Posts[0], s.Posts[1], s.Posts[2], s.Posts[4]
	if soon.Live() || later.Live() || !newer.Live() {
		t.Error("Live is wrong")
	}
	if tech := s.Threads[0]; len(tech.Posts) != 2 {
		t.Errorf("tech.Posts = %v, want no draft", tech.Posts)
	}
	if len(older.LinkedFrom) != 1 || older.LinkedFrom[0] != newer {
		t.Errorf("older.LinkedFrom = %v, want newer only", older.LinkedFrom)
	}
	if len(s.Works[0].CitedBy) != 1 || s.Works[0].CitedBy[0] != newer {
		t.Errorf("CitedBy = %v, want newer only", s.Works[0].CitedBy)
	}
}

// A thread a post names exists without threads.toml; one threads.toml lists
// exists without a post.
func TestThreadFromPost(t *testing.T) {
	c := sample()
	c.Posts[0].Threads = []string{"tech", "open-science"}
	s, err := New(c, testOptions)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, th := range s.Threads {
		names = append(names, fmt.Sprintf("%s:%v:%d", th.Name, th.Listed, len(th.Posts)))
	}
	if got := strings.Join(names, " "); got != "tech:true:2 garden:true:1 open-science:false:1" {
		t.Errorf("threads = %s", got)
	}
}

// testOptions are a post page's sections, as the example site's.
var testOptions = Options{SectionIDs: []string{"references", "linked-from", "cite"}}
