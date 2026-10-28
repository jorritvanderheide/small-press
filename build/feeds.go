package build

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"codeberg.org/BW20/small-press/content"
	"codeberg.org/BW20/small-press/render"
	"codeberg.org/BW20/small-press/site"
)

const stamp = "2006-01-02T15:04:05Z07:00"

type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Rights   string      `xml:"rights"`
	Links    []atomLink  `xml:"link"`
	Author   atomAuthor  `xml:"author"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type atomAuthor struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"` // the author's ORCID iD, as an address
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

type atomEntry struct {
	Title      string         `xml:"title"`
	ID         string         `xml:"id"`
	Link       atomLink       `xml:"link"`
	Published  string         `xml:"published"`
	Updated    string         `xml:"updated"`
	Categories []atomCategory `xml:"category"`
	Summary    string         `xml:"summary"`
	Content    atomContent    `xml:"content"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	// The post's own address, so its links and images, written relative to
	// it, resolve in a feed reader too.
	Base string `xml:"xml:base,attr"`
	Text string `xml:",chardata"`
}

// feed is the Atom feed: every live post in full, each with its address as
// its id, which never changes. It is as new as the newest post, not the
// build, so the same posts give the same feed.
func feed(s *site.Site, text func(*site.Post) (string, error)) ([]byte, error) {
	c := s.Config
	f := atomFeed{
		Title: c.Title, Subtitle: c.Description, ID: c.URL + "/",
		Links:  []atomLink{{Href: c.URL + "/feed.xml", Rel: "self"}, {Href: c.URL + "/"}},
		Author: atomAuthor{Name: c.Author.Name(), URI: orcidURL(c)},
		Rights: license,
	}
	var newest time.Time
	for _, p := range s.Posts {
		if !p.Live() {
			continue
		}
		html, err := text(p)
		if err != nil {
			return nil, err
		}
		url := c.URL + p.Path()
		updated := latest(p)
		if updated.After(newest) {
			newest = updated
		}
		// Its threads, which Rogue Scholar reads as the post's tags.
		var categories []atomCategory
		for _, t := range p.ThreadList {
			categories = append(categories, atomCategory{Term: t.Name})
		}
		f.Entries = append(f.Entries, atomEntry{
			Title: p.Title, ID: url, Link: atomLink{Href: url},
			Published: p.Date.Format(stamp), Updated: updated.Format(stamp),
			Categories: categories,
			Summary:    p.Summary,
			Content:    atomContent{Type: "html", Base: url, Text: html},
		})
	}
	// No post yet: the epoch, which the first post replaces.
	if newest.IsZero() {
		newest = time.Unix(0, 0).UTC()
	}
	f.Updated = newest.Format(stamp)
	return marshal(f)
}

// latest is when the post last changed: when it was updated, or written.
func latest(p *site.Post) time.Time {
	if p.Updated.After(p.Date) {
		return p.Updated
	}
	return p.Date
}

type urlSet struct {
	XMLName xml.Name  `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []siteURL `xml:"url"`
}

type siteURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// sitemap lists every page a search engine may show: not a withdrawn post's.
func sitemap(s *site.Site) ([]byte, error) {
	u := s.Config.URL
	set := urlSet{URLs: []siteURL{{Loc: u + "/"}, {Loc: u + "/log/"}}}
	for _, p := range s.Posts {
		if p.Live() {
			set.URLs = append(set.URLs, siteURL{Loc: u + p.Path(), LastMod: latest(p).Format("2006-01-02")})
		}
	}
	for _, p := range s.Pages {
		set.URLs = append(set.URLs, siteURL{Loc: u + p.Path()})
	}
	// A thread no live post is in only when threads.toml keeps it: a draft's
	// alone isn't announced.
	for _, t := range s.Threads {
		if len(t.Posts) > 0 || t.Listed {
			set.URLs = append(set.URLs, siteURL{Loc: u + t.Path()})
		}
	}
	for _, w := range s.Works {
		set.URLs = append(set.URLs, siteURL{Loc: u + w.Path()})
	}
	return marshal(set)
}

func marshal(v any) ([]byte, error) {
	out, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

// A crawler's name goes into a regular expression in nginx's configuration,
// so it is only what user agents are made of.
var crawlerName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// crawlers reads the list of crawlers to keep out: one user agent a line,
// with # for a comment.
func crawlers(list []byte) ([]string, error) {
	var names []string
	for i, line := range strings.Split(string(list), "\n") {
		line, _, _ = strings.Cut(line, "#")
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		if !crawlerName.MatchString(line) {
			return nil, fmt.Errorf("crawlers.txt:%d: %q isn't a user agent's name: letters, digits and hyphens", i+1, line)
		}
		names = append(names, line)
	}
	if len(names) == 0 {
		return nil, errors.New("crawlers.txt: no crawlers")
	}
	return names, nil
}

// robots asks every crawler on the list to stay away, and the others to come
// in.
func robots(s *site.Site, names []string) []byte {
	var b bytes.Buffer
	b.WriteString("User-agent: *\nAllow: /\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\nUser-agent: %s\nDisallow: /\n", n)
	}
	fmt.Fprintf(&b, "\nSitemap: %s/sitemap.xml\n", s.Config.URL)
	return b.Bytes()
}

// crawlerBlock is the 403 for the same crawlers, for those that don't read
// robots.txt, as an nginx include for a location.
func crawlerBlock(names []string) []byte {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return fmt.Appendf(nil, "if ($http_user_agent ~* \"(%s)\") {\n    return 403;\n}\n", strings.Join(quoted, "|"))
}

// aliasRedirects are the old slugs' permanent redirects, as an nginx include
// for the server. Slugs are only lowercase letters, digits and hyphens, so
// they go in as they are.
func aliasRedirects(s *site.Site) []byte {
	var b bytes.Buffer
	olds := make([]string, 0, len(s.Aliases))
	for old := range s.Aliases {
		olds = append(olds, old)
	}
	slices.Sort(olds)
	for _, old := range olds {
		fmt.Fprintf(&b, "location = /log/%s/ {\n    return 301 /log/%s/;\n}\n", old, s.Aliases[old])
	}
	return b.Bytes()
}

// PlainReference is a work's reference as plain text, for a tooltip or a
// citation_reference tag.
func PlainReference(w *site.Work) string { return render.Plain(w.Text) }

// WorkURL is where a work can be found, or "" when its reference says so
// already: a style that writes the DOI puts it in the text, as
// https://doi.org/… or doi:…, so a DOI is looked for by itself.
func WorkURL(w *site.Work) string {
	find := strings.TrimPrefix(w.URL, "https://doi.org/")
	if w.URL == "" || strings.Contains(strings.ToLower(w.Text), strings.ToLower(find)) {
		return ""
	}
	return w.URL
}

// license is what posts are under, as the feed and every page say.
const license = "CC BY 4.0: https://creativecommons.org/licenses/by/4.0/"

// orcidURL is the author's ORCID iD as an address, or "" without one.
func orcidURL(c content.Config) string {
	if c.Author.ORCID == "" {
		return ""
	}
	return "https://orcid.org/" + c.Author.ORCID
}
