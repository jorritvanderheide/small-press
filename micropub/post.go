package micropub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"codeberg.org/BW20/small-press/content"
	"codeberg.org/BW20/small-press/render"
)

// Generator marks a post the receiver wrote, and so may change.
const Generator = "micropub"

// request is a Micropub request in JSON: a create, with type and
// properties, or an action on a post by its url.
type request struct {
	Type       []string                     `json:"type"`
	Properties map[string][]json.RawMessage `json:"properties"`
	Action     string                       `json:"action"`
	URL        string                       `json:"url"`
	Reason     string                       `json:"reason"` // why a post is withdrawn
	Replace    map[string][]json.RawMessage `json:"replace"`
	Add        json.RawMessage              `json:"add"`
	Delete     json.RawMessage              `json:"delete"`
	// Overwrite says to replace a post even when it was edited elsewhere
	// since the receiver last wrote it.
	Overwrite bool `json:"mp-overwrite"`
}

// reference is one entry of the references extension.
type reference struct {
	Key  string `json:"key"`
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// frontMatter is a post's front matter as the receiver writes it.
type frontMatter struct {
	Title           string     `toml:"title"`
	Summary         string     `toml:"summary"`
	Date            time.Time  `toml:"date"`
	Updated         *time.Time `toml:"updated,omitempty"`
	Threads         []string   `toml:"threads,omitempty"`
	Aliases         []string   `toml:"aliases,omitempty"`
	Generator       string     `toml:"generator"`
	References      []string   `toml:"references,omitempty"`
	Withdrawn       *time.Time `toml:"withdrawn,omitempty"`
	WithdrawnReason string     `toml:"withdrawn-reason,omitempty"`
	DOI             string     `toml:"doi,omitempty"`
	Draft           bool       `toml:"draft,omitempty"`
}

// work is works/<key>.toml.
type work struct {
	Text string `toml:"text"`
	URL  string `toml:"url,omitempty"`
}

// props reads a request's properties: each a list, as Micropub has them.
type props map[string][]json.RawMessage

// str is the property's one string, and whether it is there.
func (p props) str(name string) (string, bool, error) {
	values, ok := p[name]
	if !ok || len(values) == 0 {
		return "", false, nil
	}
	var s string
	if len(values) > 1 || json.Unmarshal(values[0], &s) != nil {
		if name == "content" {
			return "", false, invalid("content is a plain string of Markdown: this site takes the references extension")
		}
		return "", false, invalid("%s is one string", name)
	}
	return s, true, nil
}

// strs is the property's strings.
func (p props) strs(name string) ([]string, error) {
	var all []string
	for _, v := range p[name] {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, invalid("%s is a list of strings", name)
		}
		all = append(all, s)
	}
	return all, nil
}

func (p props) references() ([]reference, bool, error) {
	values, ok := p["references"]
	if !ok {
		return nil, false, nil
	}
	refs := make([]reference, len(values))
	for i, v := range values {
		if err := json.Unmarshal(v, &refs[i]); err != nil {
			return nil, false, invalid("references: each is an object with key, text and url")
		}
		if !content.IsKey(refs[i].Key) {
			return nil, false, invalid("references: key %q isn't a citation key: ASCII letters, digits, - and _", refs[i].Key)
		}
		if refs[i].Text == "" {
			return nil, false, invalid("references: %s has no text", refs[i].Key)
		}
	}
	return refs, true, nil
}

// draft reads post-status: whether the post is a draft, and whether the
// request says.
func (p props) draft() (bool, bool, error) {
	status, ok, err := p.str("post-status")
	if !ok || err != nil {
		return false, false, err
	}
	if status != "published" && status != "draft" {
		return false, false, invalid("post-status is published or draft")
	}
	return status == "draft", true, nil
}

func (p props) time(name string) (time.Time, bool, error) {
	s, ok, err := p.str(name)
	if !ok || err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false, invalid("%s is a date and time, as 2026-03-15T10:00:00Z", name)
	}
	return t.UTC(), true, nil
}

// create makes a new post.
func (s *Server) create(fsys fs.FS, req request) (Changes, string, string, error) {
	if !slices.Equal(req.Type, []string{"h-entry"}) {
		return nil, "", "", invalid("a new post is type h-entry")
	}
	p := props(req.Properties)
	site, err := readSite(fsys)
	if err != nil {
		return nil, "", "", err
	}
	title, _, err := p.str("name")
	if err != nil {
		return nil, "", "", err
	}
	body, ok, err := p.str("content")
	if err != nil {
		return nil, "", "", err
	}
	if title == "" || !ok {
		return nil, "", "", invalid("a post needs a name, its title, and content")
	}
	slug, given, err := p.str("mp-slug")
	if err != nil {
		return nil, "", "", err
	}
	if !given {
		slug = slugOf(title)
	}
	if !content.IsName(slug) {
		return nil, "", "", invalid("slug %q is lowercase letters, digits and hyphens", slug)
	}
	for _, post := range site.Posts {
		if post.Slug == slug || slices.Contains(post.Aliases, slug) {
			return nil, "", "", invalid("slug %s is taken, and a slug never changes: give the post another (mp-slug)", slug)
		}
	}

	fm := frontMatter{Title: title, Generator: Generator, Date: s.Now().UTC().Truncate(time.Second)}
	if fm.Summary, _, err = p.str("summary"); err != nil {
		return nil, "", "", err
	}
	if fm.Threads, err = p.strs("category"); err != nil {
		return nil, "", "", err
	}
	if t, ok, err := p.time("published"); err != nil {
		return nil, "", "", err
	} else if ok {
		fm.Date = t
	}
	if t, ok, err := p.time("updated"); err != nil {
		return nil, "", "", err
	} else if ok {
		fm.Updated = optional(t)
	}
	if fm.Draft, _, err = p.draft(); err != nil {
		return nil, "", "", err
	}
	changes := Changes{}
	if err := s.setReferences(changes, &fm, p); err != nil {
		return nil, "", "", err
	}
	if err := s.writePost(changes, slug, fm, body, nil); err != nil {
		return nil, "", "", err
	}
	if fm.Draft {
		return changes, "draft: " + slug, slug, nil
	}
	return changes, "publish: " + slug, slug, nil
}

// find is the post at url, by its slug.
func (s *Server) find(site *content.Site, url string) (content.Post, error) {
	slug, ok := strings.CutPrefix(url, s.BaseURL+"/log/")
	slug = strings.TrimSuffix(slug, "/")
	i := slices.IndexFunc(site.Posts, func(p content.Post) bool { return p.Slug == slug })
	if !ok || i < 0 {
		return content.Post{}, invalid("there's no post at %s", url)
	}
	return site.Posts[i], nil
}

// edit updates, withdraws or restores a post the receiver wrote, publishes
// a draft, or discards one.
func (s *Server) edit(fsys fs.FS, req request) (Changes, string, string, error) {
	site, err := readSite(fsys)
	if err != nil {
		return nil, "", "", err
	}
	post, err := s.find(site, req.URL)
	if err != nil {
		return nil, "", "", err
	}
	slug := post.Slug
	if post.Generator != Generator {
		return nil, "", "", forbidden("log/%s is written by hand, so it's changed by hand", slug)
	}
	fm := frontMatter{
		Title: post.Title, Summary: post.Summary, Date: post.Date, Updated: optional(post.Updated),
		Threads: post.Threads, Aliases: post.Aliases, Generator: Generator,
		References: post.References, Withdrawn: optional(post.Withdrawn), WithdrawnReason: post.WithdrawnReason,
		DOI: post.DOI, Draft: post.Draft,
	}
	body := string(post.Body)
	changes := Changes{}

	switch {
	case req.Action == "delete" && post.Draft:
		// Never public, so nothing links to it or cites it: it goes.
		changes["log/"+slug+"/index.md"] = nil
		for name := range post.Images {
			changes["log/"+slug+"/"+name] = nil
		}
		return changes, "discard: " + slug, slug, nil
	case req.Action == "undelete" && post.Draft:
		return nil, "", "", invalid("log/%s is a draft, which is never withdrawn, so there's nothing to restore", slug)
	}
	switch req.Action {
	case "delete":
		fm.Withdrawn = optional(s.Now().UTC().Truncate(time.Second))
		fm.WithdrawnReason = strings.Join(strings.Fields(req.Reason), " ")
		return changes, "withdraw: " + slug, slug, s.writePost(changes, slug, fm, body, post.Images)
	case "undelete":
		fm.Withdrawn, fm.WithdrawnReason = nil, ""
		return changes, "restore: " + slug, slug, s.writePost(changes, slug, fm, body, post.Images)
	}

	// An update replaces the whole post, so one edited by hand since, or by
	// another tool, is refused unless the request says to replace it: the
	// person publishing is asked, rather than losing the edit without a word.
	if !req.Overwrite {
		commit, err := s.Repo.EditedElsewhere("log/" + slug + "/index.md")
		if err != nil {
			return nil, "", "", err
		}
		if commit != "" {
			return nil, "", "", conflict("log/%s was edited elsewhere since it was last published (commit %s): publishing the note again replaces that edit", slug, commit)
		}
	}
	if len(req.Add) > 0 || len(req.Delete) > 0 {
		return nil, "", "", invalid("an update replaces what changed (replace): add and delete aren't taken")
	}
	p := props(req.Replace)
	for name := range p {
		if !slices.Contains([]string{"name", "summary", "content", "category", "references", "updated", "post-status"}, name) {
			return nil, "", "", invalid("%s can't be replaced: a post's slug and date never change", name)
		}
	}
	if t, ok, err := p.str("name"); err != nil {
		return nil, "", "", err
	} else if ok {
		fm.Title = t
	}
	if t, ok, err := p.str("summary"); err != nil {
		return nil, "", "", err
	} else if ok {
		fm.Summary = t
	}
	if t, ok, err := p.str("content"); err != nil {
		return nil, "", "", err
	} else if ok {
		body = t
	}
	if _, ok := p["category"]; ok {
		if fm.Threads, err = p.strs("category"); err != nil {
			return nil, "", "", err
		}
	}
	if t, ok, err := p.time("updated"); err != nil {
		return nil, "", "", err
	} else if ok {
		fm.Updated = optional(t)
	}
	if err := s.setReferences(changes, &fm, p); err != nil {
		return nil, "", "", err
	}
	message := "update: " + slug
	draft, ok, err := p.draft()
	switch {
	case err != nil:
		return nil, "", "", err
	case ok && draft && !post.Draft:
		return nil, "", "", invalid("log/%s is published, and a published post doesn't become a draft again: withdraw it instead", slug)
	case ok && !draft && post.Draft:
		// Its date is when it's published, and it hasn't been updated since.
		fm.Draft, fm.Date, fm.Updated = false, s.Now().UTC().Truncate(time.Second), nil
		message = "publish: " + slug
	}
	return changes, message, slug, s.writePost(changes, slug, fm, body, post.Images)
}

// source answers q=source for a post: its properties as the site keeps them,
// or only those asked for with properties[]. Its content is the text as it is
// in the repository, its images by their names next to it, and its references
// are the works' files as they are now.
func (s *Server) source(fsys fs.FS, url string, wanted []string) (map[string]any, error) {
	site, err := readSite(fsys)
	if err != nil {
		return nil, err
	}
	post, err := s.find(site, url)
	if err != nil {
		return nil, err
	}
	status := "published"
	if post.Draft {
		status = "draft"
	}
	refs := []reference{}
	for _, key := range post.References {
		if i := slices.IndexFunc(site.Works, func(w content.Work) bool { return w.Key == key }); i >= 0 {
			refs = append(refs, reference{Key: key, Text: site.Works[i].Text, URL: site.Works[i].URL})
		}
	}
	props := map[string]any{
		"name":        []string{post.Title},
		"summary":     []string{post.Summary},
		"content":     []string{strings.TrimSpace(string(post.Body))},
		"category":    append([]string{}, post.Threads...),
		"references":  refs,
		"post-status": []string{status},
	}
	if len(wanted) == 0 {
		return map[string]any{"type": []string{"h-entry"}, "properties": props}, nil
	}
	only := map[string]any{}
	for _, name := range wanted {
		if value, ok := props[name]; ok {
			only[name] = value
		}
	}
	return map[string]any{"properties": only}, nil
}

// setReferences takes the references extension: the keys in the post's
// front matter, and each work's file, which the last post to cite it sets.
func (s *Server) setReferences(changes Changes, fm *frontMatter, p props) error {
	refs, ok, err := p.references()
	if err != nil || !ok {
		return err
	}
	fm.References = nil
	for _, r := range refs {
		fm.References = append(fm.References, r.Key)
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(work{Text: r.Text, URL: r.URL}); err != nil {
			return err
		}
		changes["works/"+r.Key+".toml"] = buf.Bytes()
	}
	return nil
}

// writePost puts the post's index.md in changes, with its links made the
// site's own, the images it uploaded next to it, and the images it no longer
// has taken away.
func (s *Server) writePost(changes Changes, slug string, fm frontMatter, body string, had map[string][]byte) error {
	dir := "log/" + slug + "/"
	body, uploads, err := s.rewrite(body)
	if err != nil {
		return err
	}
	for name, data := range uploads {
		changes[dir+name] = data
	}
	used := map[string]bool{}
	for _, l := range render.Parse([]byte(body)).Links() {
		if l.Image {
			used[l.Dest] = true
		}
	}
	for name := range had {
		if !used[name] {
			changes[dir+name] = nil
		}
	}
	var buf bytes.Buffer
	buf.WriteString("+++\n")
	if err := toml.NewEncoder(&buf).Encode(fm); err != nil {
		return err
	}
	buf.WriteString("+++\n\n")
	buf.WriteString(strings.TrimSpace(body))
	buf.WriteString("\n")
	changes[dir+"index.md"] = buf.Bytes()
	return nil
}

// An image's address in Markdown, ![alt](address), with or without the <…>
// pandoc puts around an address with a space or a parenthesis in it.
var imageDest = regexp.MustCompile(`(!\[(?:[^\]\\]|\\.)*\]\()(<[^>]*>|[^)\s]+)\)`)

// rewrite puts each image the media endpoint holds next to the post, by a
// name of its own, and links it by that name. Everything else in the text is
// kept as it comes: citations, #ref-<key>, and links to the site's own posts
// are what the site reads already. It returns the images it took, by their
// names.
func (s *Server) rewrite(body string) (string, map[string][]byte, error) {
	uploads := map[string][]byte{}
	var errs []error
	media := s.BaseURL + mediaEndpoint + "/"
	body = imageDest.ReplaceAllStringFunc(body, func(m string) string {
		parts := imageDest.FindStringSubmatch(m)
		dest := strings.TrimSuffix(strings.TrimPrefix(parts[2], "<"), ">")
		name, ok := strings.CutPrefix(dest, media)
		if !ok {
			return m
		}
		data, err := s.staged(name)
		if err != nil {
			errs = append(errs, err)
			return m
		}
		file := fileName(name)
		uploads[file] = data
		return parts[1] + file + ")"
	})
	return body, uploads, errors.Join(errs...)
}

// optional is a date the front matter leaves out when it's zero.
func optional(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// readSite reads content/ as it is in the repository, which builds.
func readSite(fsys fs.FS) (*content.Site, error) {
	site, err := content.Read(fsys)
	if err != nil {
		return nil, fmt.Errorf("the site doesn't read as it is in the repository: %w", err)
	}
	return site, nil
}

// slugOf makes a slug of a title: "The NixOS module system" is
// the-nixos-module-system.
func slugOf(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	return b.String()
}

// fileName is the name an uploaded image gets next to its post: the start of
// its hash, which is its name in the media folder, and its extension.
func fileName(staged string) string {
	ext := path.Ext(staged)
	base := strings.TrimSuffix(staged, ext)
	return base[:min(len(base), 12)] + ext
}
