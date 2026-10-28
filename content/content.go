// Package content reads content/: the site's facts, its posts, pages,
// threads and works. It knows nothing about HTML, and reads only the fs.FS
// it is given, so every decision is tested on an in-memory site.
//
// Each file is checked on its own here: what it must have, and that it has
// nothing the site doesn't know. What needs more than one file, such as a
// link to another post, is checked in package site.
package content

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Site is everything in content/.
type Site struct {
	Config  Config
	Posts   []Post // by slug
	Pages   []Page // by name
	Threads []Thread
	Works   []Work // by key
}

// Config is site.toml.
type Config struct {
	Title       string     `toml:"title"`
	URL         string     `toml:"url"` // https://, without a slash at the end
	Description string     `toml:"description"`
	Author      Author     `toml:"author"`
	Menu        []MenuItem `toml:"menu"`
}

// Author is who every post is by.
type Author struct {
	Given  string `toml:"given"`
	Family string `toml:"family"`
	ORCID  string `toml:"orcid"` // 0009-0005-6464-7721, optional
}

// Name is the author's name as it is written: "Jorrit van der Heide".
func (a Author) Name() string { return a.Given + " " + a.Family }

// MenuItem is one entry in the menu: its words, and where it goes.
type MenuItem struct {
	Name string `toml:"name"`
	Link string `toml:"link"`
}

// A Post is log/<slug>/index.md: its front matter, as docs/architecture.md
// describes it, its Markdown body, and the images next to it.
type Post struct {
	Slug string `toml:"-"`

	Title           string    `toml:"title"`
	Summary         string    `toml:"summary"`
	Date            time.Time `toml:"date"`
	Updated         time.Time `toml:"updated"`
	Threads         []string  `toml:"threads"`
	Aliases         []string  `toml:"aliases"`
	Generator       string    `toml:"generator"`
	References      []string  `toml:"references"`
	Withdrawn       time.Time `toml:"withdrawn"`
	WithdrawnReason string    `toml:"withdrawn-reason"` // why, for its readers
	DOI             string    `toml:"doi"`              // 10.64000/…, once it has one
	Draft           bool      `toml:"draft"`

	Body   []byte            `toml:"-"`
	Images map[string][]byte `toml:"-"` // by file name
}

// A Page is pages/<name>.md, at /<name>/.
type Page struct {
	Name  string `toml:"-"`
	Title string `toml:"title"`
	Body  []byte `toml:"-"`
}

// A Thread is one of threads.toml's. A thread exists while a post names it;
// threads.toml only describes one, puts it in order, or keeps its page when
// no post names it any more.
type Thread struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// A Work is works/<key>.toml: a work posts cite, formatted once.
type Work struct {
	Key  string `toml:"-"`
	Text string `toml:"text"` // inline Markdown: emphasis and links
	URL  string `toml:"url"`
}

var (
	// A slug, a page's name and a thread's name: they are in URLs, and in
	// the nginx includes the build writes.
	namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// A work's citation key, as Better BibTeX writes it.
	keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	// A DOI, as it is registered: 10.64000/abcd-1234.
	doiPattern = regexp.MustCompile(`^10\.\d{4,9}/\S+$`)
	// An ORCID iD's shape; its last character is a check digit (IsORCID).
	orcidPattern = regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)
)

// IsName says whether s can be a slug, a page's name or a thread's name.
func IsName(s string) bool { return namePattern.MatchString(s) }

// IsKey says whether s can be a work's citation key.
func IsKey(s string) bool { return keyPattern.MatchString(s) }

// Images a post may have next to it, by extension.
var imageExts = []string{".png", ".jpg", ".jpeg"}

// Read reads all of content/. Everything that can't be read, and anything
// that doesn't belong there, is in the error, each by its file.
func Read(fsys fs.FS) (*Site, error) {
	s := &Site{}
	var errs []error
	fail := func(name string, err error) { errs = append(errs, fmt.Errorf("%s: %w", name, err)) }

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		switch name := e.Name(); {
		case name == "site.toml" && !e.IsDir():
			if err := decodeFile(fsys, name, &s.Config); err != nil {
				fail(name, err)
			} else if err := s.Config.check(); err != nil {
				fail(name, err)
			}
		case name == "threads.toml" && !e.IsDir():
			threads, err := readThreads(fsys, name)
			if err != nil {
				fail(name, err)
			}
			s.Threads = threads
		case name == "log" && e.IsDir():
			s.Posts = readAll(fsys, name, readPost, fail)
		case name == "pages" && e.IsDir():
			s.Pages = readAll(fsys, name, readPage, fail)
		case name == "works" && e.IsDir():
			s.Works = readAll(fsys, name, readWork, fail)
		default:
			fail(name, errors.New("doesn't belong in content/: see docs/architecture.md, Content"))
		}
	}
	if _, err := fs.Stat(fsys, "site.toml"); err != nil {
		fail("site.toml", errors.New("missing"))
	}
	return s, errors.Join(errs...)
}

// ThreadNames is every thread: threads.toml's, in its order, then the others
// a post names, by name.
func (s *Site) ThreadNames() []string {
	names := []string{}
	seen := map[string]bool{}
	for _, t := range s.Threads {
		names = append(names, t.Name)
		seen[t.Name] = true
	}
	var named []string
	for _, p := range s.Posts {
		for _, name := range p.Threads {
			if !seen[name] {
				seen[name] = true
				named = append(named, name)
			}
		}
	}
	slices.Sort(named)
	return append(names, named...)
}

// readAll reads each entry of dir with read, which knows what an entry must
// be, and returns what it read, sorted as the entries are (by name).
func readAll[T any](fsys fs.FS, dir string, read func(fs.FS, fs.DirEntry, string) (T, error), fail func(string, error)) []T {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		fail(dir, err)
		return nil
	}
	var all []T
	for _, e := range entries {
		name := path.Join(dir, e.Name())
		v, err := read(fsys, e, name)
		if err != nil {
			// Each of an entry's mistakes on a line of its own, by its name.
			if joined, ok := err.(interface{ Unwrap() []error }); ok {
				for _, err := range joined.Unwrap() {
					fail(name, err)
				}
			} else {
				fail(name, err)
			}
			continue
		}
		all = append(all, v)
	}
	return all
}

func readPost(fsys fs.FS, e fs.DirEntry, dir string) (Post, error) {
	if !e.IsDir() {
		return Post{}, errors.New("a post is a folder, log/<slug>/, with its text in index.md")
	}
	if !namePattern.MatchString(e.Name()) {
		return Post{}, errors.New("a slug is lowercase letters, digits and hyphens")
	}
	p := Post{Slug: e.Name(), Images: map[string][]byte{}}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return Post{}, err
	}
	var errs []error
	index := false
	for _, f := range entries {
		name := path.Join(dir, f.Name())
		switch {
		case f.Name() == "index.md" && !f.IsDir():
			index = true
			body, err := readFrontMatter(fsys, name, &p)
			if err != nil {
				errs = append(errs, fmt.Errorf("index.md: %w", err))
				continue
			}
			p.Body = body
			if err := required(map[string]bool{"title": p.Title != "", "summary": p.Summary != "", "date": !p.Date.IsZero()}); err != nil {
				errs = append(errs, fmt.Errorf("index.md: %w", err))
			}
			if p.DOI != "" && !doiPattern.MatchString(p.DOI) {
				errs = append(errs, fmt.Errorf("index.md: doi %q isn't a DOI: 10.<number>/<suffix>, without https://doi.org/", p.DOI))
			}
			if p.WithdrawnReason != "" && p.Withdrawn.IsZero() {
				errs = append(errs, errors.New("index.md: withdrawn-reason without withdrawn: a reason goes with a withdrawal"))
			}
			if p.Draft && !p.Withdrawn.IsZero() {
				errs = append(errs, errors.New("index.md: a draft was never published, so it isn't withdrawn: delete its folder instead"))
			}
		case !f.IsDir() && slices.Contains(imageExts, strings.ToLower(path.Ext(f.Name()))):
			data, err := fs.ReadFile(fsys, name)
			if err != nil {
				errs = append(errs, err)
			}
			p.Images[f.Name()] = data
		default:
			errs = append(errs, fmt.Errorf("%s doesn't belong in a post's folder: only index.md and its images (%s)", f.Name(), strings.Join(imageExts, ", ")))
		}
	}
	if !index {
		errs = append(errs, errors.New("no index.md"))
	}
	if err := errors.Join(errs...); err != nil {
		return Post{}, err
	}
	return p, nil
}

func readPage(fsys fs.FS, e fs.DirEntry, name string) (Page, error) {
	base, ok := strings.CutSuffix(e.Name(), ".md")
	if e.IsDir() || !ok {
		return Page{}, errors.New("a page is pages/<name>.md")
	}
	if !namePattern.MatchString(base) {
		return Page{}, errors.New("a page's name is lowercase letters, digits and hyphens")
	}
	p := Page{Name: base}
	body, err := readFrontMatter(fsys, name, &p)
	if err != nil {
		return Page{}, err
	}
	p.Body = body
	if err := required(map[string]bool{"title": p.Title != ""}); err != nil {
		return Page{}, err
	}
	return p, nil
}

func readWork(fsys fs.FS, e fs.DirEntry, name string) (Work, error) {
	key, ok := strings.CutSuffix(e.Name(), ".toml")
	if e.IsDir() || !ok {
		return Work{}, errors.New("a work is works/<key>.toml")
	}
	if !keyPattern.MatchString(key) {
		return Work{}, errors.New("a citation key is ASCII letters, digits, - and _")
	}
	w := Work{Key: key}
	if err := decodeFile(fsys, name, &w); err != nil {
		return Work{}, err
	}
	if err := required(map[string]bool{"text": w.Text != ""}); err != nil {
		return Work{}, err
	}
	return w, nil
}

func readThreads(fsys fs.FS, name string) ([]Thread, error) {
	var file struct {
		Threads []Thread `toml:"threads"`
	}
	if err := decodeFile(fsys, name, &file); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var errs []error
	for i, t := range file.Threads {
		switch {
		case !namePattern.MatchString(t.Name):
			errs = append(errs, fmt.Errorf("thread %d: a thread's name is lowercase letters, digits and hyphens", i+1))
		case seen[t.Name]:
			errs = append(errs, fmt.Errorf("thread %s: listed twice", t.Name))
		}
		seen[t.Name] = true
	}
	return file.Threads, errors.Join(errs...)
}

func (c Config) check() error {
	err := required(map[string]bool{
		"title":         c.Title != "",
		"url":           c.URL != "",
		"description":   c.Description != "",
		"author.given":  c.Author.Given != "",
		"author.family": c.Author.Family != "",
	})
	if err != nil {
		return err
	}
	if !strings.HasPrefix(c.URL, "https://") || strings.HasSuffix(c.URL, "/") {
		return errors.New("url is the site's https:// address, without a slash at the end")
	}
	if c.Author.ORCID != "" && !IsORCID(c.Author.ORCID) {
		return fmt.Errorf("author.orcid %q isn't an ORCID iD: four groups of four digits, as 0009-0005-6464-7721, whose last digit checks the others", c.Author.ORCID)
	}
	for _, m := range c.Menu {
		if m.Name == "" || !strings.HasPrefix(m.Link, "/") {
			return errors.New("each menu entry has a name, and a link on the site, starting with /")
		}
	}
	return nil
}

// required says which of the fields, by name, are missing.
func required(have map[string]bool) error {
	var missing []string
	for name, ok := range have {
		if !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return fmt.Errorf("missing %s", strings.Join(missing, ", "))
}

// fence opens and closes the front matter, each on a line of its own.
const fence = "+++"

// readFrontMatter decodes the front matter of the Markdown file name into v,
// and returns the body after it.
func readFrontMatter(fsys fs.FS, name string, v any) ([]byte, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(src, []byte(fence+"\n"))
	if !ok {
		return nil, errors.New("no front matter: the file has to start with a " + fence + " line")
	}
	// A newline in front, so empty front matter has its closing line found too.
	rest = append([]byte("\n"), rest...)
	front, body, ok := bytes.Cut(rest, []byte("\n"+fence+"\n"))
	if !ok {
		if front, ok = bytes.CutSuffix(rest, []byte("\n"+fence)); !ok {
			return nil, errors.New("the front matter has no closing " + fence + " line")
		}
	}
	if err := decode(front, v); err != nil {
		return nil, err
	}
	if body == nil {
		body = []byte{}
	}
	return body, nil
}

func decodeFile(fsys fs.FS, name string, v any) error {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	return decode(data, v)
}

// decode decodes TOML into v. A field the site doesn't know is a mistake,
// not something to ignore: a typo would otherwise drop what it was meant to
// set.
func decode(data []byte, v any) error {
	meta, err := toml.Decode(string(data), v)
	if err != nil {
		return err
	}
	if unknown := meta.Undecoded(); len(unknown) > 0 {
		keys := make([]string, len(unknown))
		for i, k := range unknown {
			keys[i] = k.String()
		}
		return fmt.Errorf("unknown field %s", strings.Join(keys, ", "))
	}
	return nil
}

// IsORCID says whether s is an ORCID iD: its shape, and its check digit, ISO
// 7064 MOD 11-2 over the fifteen digits before it.
func IsORCID(s string) bool {
	if !orcidPattern.MatchString(s) {
		return false
	}
	digits := strings.ReplaceAll(s, "-", "")
	total := 0
	for _, d := range digits[:15] {
		total = (total + int(d-'0')) * 2
	}
	check := (12 - total%11) % 11
	want := byte('0' + check)
	if check == 10 {
		want = 'X'
	}
	return digits[15] == want
}
