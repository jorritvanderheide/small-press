package content

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const siteTOML = `title = 'BW20'
url = 'https://bw20.nl'
description = 'A small, finite website.'

[author]
given = 'Jorrit'
family = 'van der Heide'

[[menu]]
name = 'log'
link = '/log/'
`

// site is a whole content/, as it should be; a test changes what it tests.
func site() fstest.MapFS {
	return fstest.MapFS{
		"site.toml": {Data: []byte(siteTOML)},
		"threads.toml": {Data: []byte(`[[threads]]
name = 'tech'
description = 'Notes on NixOS.'
`)},
		"log/quiet/index.md": {Data: []byte(`+++
title = 'The NixOS module system is a quiet revolution'
summary = 'Why declarative configuration changes how we think about infrastructure.'
date = 2026-03-15T10:00:00Z
threads = ['tech']
references = ['marsh2020']
+++

Most people encounter NixOS and get lost in the syntax.
`)},
		"log/quiet/figure.png": {Data: []byte("png")},
		"log/another/index.md": {Data: []byte("+++\ntitle = 'A'\nsummary = 'B'\ndate = 2025-01-01T00:00:00Z\n+++\n")},
		"pages/about.md":       {Data: []byte("+++\ntitle = 'About'\n+++\n\nWho, why.\n")},
		"works/marsh2020.toml": {Data: []byte("text = 'Marsh, Ann. 2020. *Archives*.'\nurl = 'https://doi.org/10.1/x'\n")},
	}
}

func TestRead(t *testing.T) {
	s, err := Read(site())
	if err != nil {
		t.Fatal(err)
	}
	if s.Config.Title != "BW20" || s.Config.Author.Name() != "Jorrit van der Heide" || len(s.Config.Menu) != 1 {
		t.Errorf("Config = %+v", s.Config)
	}
	if len(s.Threads) != 1 || s.Threads[0].Name != "tech" {
		t.Errorf("Threads = %+v", s.Threads)
	}
	if len(s.Posts) != 2 || s.Posts[0].Slug != "another" || s.Posts[1].Slug != "quiet" {
		t.Fatalf("Posts = %+v, want another and quiet, by slug", s.Posts)
	}
	p := s.Posts[1]
	if want := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC); !p.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", p.Date, want)
	}
	if len(p.References) != 1 || p.References[0] != "marsh2020" {
		t.Errorf("References = %v", p.References)
	}
	if want := "\nMost people encounter NixOS and get lost in the syntax.\n"; string(p.Body) != want {
		t.Errorf("Body = %q, want %q", p.Body, want)
	}
	if string(p.Images["figure.png"]) != "png" {
		t.Errorf("Images = %v", p.Images)
	}
	if len(s.Pages) != 1 || s.Pages[0].Name != "about" || s.Pages[0].Title != "About" {
		t.Errorf("Pages = %+v", s.Pages)
	}
	if len(s.Works) != 1 || s.Works[0].Key != "marsh2020" || s.Works[0].URL != "https://doi.org/10.1/x" {
		t.Errorf("Works = %+v", s.Works)
	}
}

// Everything wrong in content/ fails, with its file and what is wrong, all
// of it at once.
func TestReadErrors(t *testing.T) {
	tests := []struct {
		file, src, want string
	}{
		{"log/missing/index.md", "+++\ntitle = 'A'\n+++\nText", "log/missing: index.md: missing date, summary"},
		{"log/unknown/index.md", "+++\ntitle = 'A'\nsummary = 'B'\ndate = 2025-01-01T00:00:00Z\nlastmod = 2025-01-01T00:00:00Z\n+++\n", "log/unknown: index.md: unknown field lastmod"},
		{"log/unopened/index.md", "title = 'A'\n", "log/unopened: index.md: no front matter"},
		{"log/unclosed/index.md", "+++\ntitle = 'A'\n", "log/unclosed: index.md: the front matter has no closing +++ line"},
		{"log/invalid/index.md", "+++\ntitle = \n+++\n", "log/invalid: index.md: toml:"},
		{"log/empty/notes.md", "", "log/empty: notes.md doesn't belong in a post's folder"},
		{"log/Capital/index.md", "", "log/Capital: a slug is lowercase letters"},
		{"log/_index.md", "", "log/_index.md: a post is a folder"},
		{"pages/about/index.md", "", "pages/about: a page is pages/<name>.md"},
		{"pages/now.md", "+++\n+++\n", "pages/now.md: missing title"},
		{"works/bad key.toml", "text = 'A'", "works/bad key.toml: a citation key is"},
		{"works/nada.toml", "url = 'https://example.org'", "works/nada.toml: missing text"},
		{"_index.md", "", "_index.md: doesn't belong in content/"},
	}
	fsys := site()
	for _, tt := range tests {
		fsys[tt.file] = &fstest.MapFile{Data: []byte(tt.src)}
	}
	_, err := Read(fsys)
	if err == nil {
		t.Fatal("no error")
	}
	for _, tt := range tests {
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("error doesn't say %q:\n%v", tt.want, err)
		}
	}
	if strings.Contains(err.Error(), "log/quiet") {
		t.Errorf("a post without mistakes is in the error:\n%v", err)
	}
}

func TestReadSiteTOML(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"missing", "", "site.toml: missing"},
		{"fields", "title = 'BW20'\n", "site.toml: missing author.family, author.given, description, url"},
		{"url", strings.Replace(siteTOML, "https://bw20.nl", "http://bw20.nl/", 1), "site.toml: url is the site's https:// address"},
		{"menu", siteTOML + "\n[[menu]]\nname = 'x'\nlink = 'x'\n", "site.toml: each menu entry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := site()
			if tt.name == "missing" {
				delete(fsys, "site.toml")
			} else {
				fsys["site.toml"] = &fstest.MapFile{Data: []byte(tt.src)}
			}
			_, err := Read(fsys)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one saying %q", err, tt.want)
			}
		})
	}
}

func TestReadThreads(t *testing.T) {
	fsys := site()
	fsys["threads.toml"] = &fstest.MapFile{Data: []byte(`[[threads]]
name = 'tech'
description = 'A'

[[threads]]
name = 'tech'
description = 'B'

[[threads]]
name = 'Garden'
description = 'C'

[[threads]]
name = 'quiet'
`)}
	_, err := Read(fsys)
	for _, want := range []string{"thread tech: listed twice", "thread 3: a thread's name is lowercase"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want one saying %q", err, want)
		}
	}
	// A thread without a description is fine: threads.toml only keeps it.
	if err != nil && strings.Contains(err.Error(), "quiet") {
		t.Errorf("err = %v, wants a description", err)
	}
}

// Every thread: threads.toml's in its order, then the ones only posts name.
func TestThreadNames(t *testing.T) {
	s := &Site{
		Threads: []Thread{{Name: "tech"}, {Name: "garden"}},
		Posts:   []Post{{Threads: []string{"tech", "open-science"}}, {Threads: []string{"art", "open-science"}}},
	}
	if got := strings.Join(s.ThreadNames(), " "); got != "tech garden art open-science" {
		t.Errorf("ThreadNames = %s", got)
	}
}

func TestIsORCID(t *testing.T) {
	for _, id := range []string{"0009-0005-6464-7721", "0000-0002-1825-0097", "0000-0001-5109-3700", "0000-0002-1694-233X"} {
		if !IsORCID(id) {
			t.Errorf("IsORCID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"0009-0005-6464-7722", "0000-0002-1825-0098", "0009000564647721", "https://orcid.org/0009-0005-6464-7721", ""} {
		if IsORCID(id) {
			t.Errorf("IsORCID(%q) = true, want false", id)
		}
	}
}

func TestReadWithdrawnDraft(t *testing.T) {
	fsys := site()
	fsys["log/draft/index.md"] = &fstest.MapFile{Data: []byte("+++\ntitle = 'A'\nsummary = 'B'\ndate = 2025-01-01T00:00:00Z\ndraft = true\nwithdrawn = 2025-02-01T00:00:00Z\n+++\n")}
	if _, err := Read(fsys); err == nil || !strings.Contains(err.Error(), "log/draft: index.md: a draft was never published") {
		t.Errorf("err = %v", err)
	}
}

func TestReadReasonWithoutWithdrawn(t *testing.T) {
	fsys := site()
	fsys["log/why/index.md"] = &fstest.MapFile{Data: []byte("+++\ntitle = 'A'\nsummary = 'B'\ndate = 2025-01-01T00:00:00Z\nwithdrawn-reason = 'Wrong.'\n+++\n")}
	if _, err := Read(fsys); err == nil || !strings.Contains(err.Error(), "log/why: index.md: withdrawn-reason without withdrawn") {
		t.Errorf("err = %v", err)
	}
}

func TestReadORCIDAndDOI(t *testing.T) {
	fsys := site()
	fsys["site.toml"] = &fstest.MapFile{Data: []byte(strings.Replace(siteTOML, "family = 'van der Heide'", "family = 'van der Heide'\norcid = '0009-0005-6464-7722'", 1))}
	fsys["log/doi/index.md"] = &fstest.MapFile{Data: []byte("+++\ntitle = 'A'\nsummary = 'B'\ndate = 2025-01-01T00:00:00Z\ndoi = 'https://doi.org/10.64000/x'\n+++\n")}
	_, err := Read(fsys)
	for _, want := range []string{"site.toml: author.orcid \"0009-0005-6464-7722\" isn't an ORCID iD", `log/doi: index.md: doi "https://doi.org/10.64000/x" isn't a DOI`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want one saying %q", err, want)
		}
	}

	fsys = site()
	fsys["site.toml"] = &fstest.MapFile{Data: []byte(strings.Replace(siteTOML, "family = 'van der Heide'", "family = 'van der Heide'\norcid = '0009-0005-6464-7721'", 1))}
	fsys["log/doi/index.md"] = &fstest.MapFile{Data: []byte("+++\ntitle = 'A'\nsummary = 'B'\ndate = 2025-01-01T00:00:00Z\ndoi = '10.64000/abcd-1234'\n+++\n")}
	s, err := Read(fsys)
	if err != nil {
		t.Fatal(err)
	}
	// By slug: another, doi, quiet.
	if s.Config.Author.ORCID != "0009-0005-6464-7721" || s.Posts[1].DOI != "10.64000/abcd-1234" {
		t.Errorf("ORCID %q, DOI %q", s.Config.Author.ORCID, s.Posts[1].DOI)
	}
}
