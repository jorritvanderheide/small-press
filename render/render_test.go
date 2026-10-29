package render

import (
	"strings"
	"testing"
)

func TestHTML(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
		not       []string
	}{
		{
			name: "text",
			src:  "A *quiet* revolution.",
			want: []string{"<p>A <em>quiet</em> revolution.</p>"},
		},
		{
			name: "table",
			src:  "| a | b |\n| --- | --- |\n| 1 | 2 |",
			want: []string{"<table>", "<td>1</td>"},
		},
		{
			name: "aligned table",
			src:  "| a |\n| ---: |\n| 1 |",
			want: []string{`<td align="right">1</td>`},
			not:  []string{"style="},
		},
		{
			name: "strikethrough",
			src:  "~~gone~~",
			want: []string{"<del>gone</del>"},
		},
		{
			name: "footnote",
			src:  "A claim.[^1]\n\n[^1]: Its source.",
			want: []string{`<sup id="fnref:1">`, "Its source."},
		},
		{
			name: "task list",
			src:  "- [ ] open\n- [x] done",
			want: []string{`<li><input disabled="" type="checkbox"> open</li>`, `<li><input checked="" disabled="" type="checkbox"> done</li>`},
			not:  []string{"[ ]", "[x]"},
		},
		{
			name: "raw HTML stays out",
			src:  "<script>alert(1)</script>\n\nText <b>bold</b>.",
			want: []string{"Text"},
			not:  []string{"<script>", "<b>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.src)).HTML(Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(string(got), w) {
					t.Errorf("no %q in\n%s", w, got)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(string(got), n) {
					t.Errorf("%q in\n%s", n, got)
				}
			}
		})
	}
}

func TestHTMLOptions(t *testing.T) {
	doc := Parse([]byte("![A garden](garden.jpg)\n\nAs [(Marsh 2020, 12)](/works/marsh2020/) says, and [this](/log/quiet/)."))
	got, err := doc.HTML(Options{
		Image: func(dest string) string { return strings.TrimSuffix(dest, ".jpg") + ".png" },
		Title: func(dest string) string {
			if dest == "/works/marsh2020/" {
				return "Marsh, Ann. 2020. Archives."
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<img src="garden.png" alt="A garden">`,
		`<a href="/works/marsh2020/" title="Marsh, Ann. 2020. Archives.">(Marsh 2020, 12)</a>`,
		`<a href="/log/quiet/">this</a>`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	// The options don't change the text itself.
	if again, _ := doc.HTML(Options{}); !strings.Contains(string(again), `src="garden.jpg"`) {
		t.Errorf("the options stayed:\n%s", again)
	}
}

func TestLinks(t *testing.T) {
	got := Parse([]byte("See [the *other* post](/log/other/), <https://example.org>, and ![a photo](photo.jpg).")).Links()
	want := []Link{
		{Dest: "/log/other/", Words: "the other post"},
		{Dest: "https://example.org", Words: "https://example.org"},
		{Dest: "photo.jpg", Words: "a photo", Image: true},
	}
	if len(got) != len(want) {
		t.Fatalf("Links = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestProblems(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"fine", "## A\n\n### B\n\n## C\n\n[x](https://example.org) [y](/log/a/) [z](#b) [m](mailto:a@b.nl) ![alt](a.png)", ""},
		{"level 1", "# Title again", "is a level 1 heading"},
		{"skip", "## A\n\n#### B", `heading "B" skips a level`},
		{"first skip", "### A", `heading "A" skips a level`},
		{"raw HTML", "<div>x</div>", "raw HTML is left out"},
		{"inline HTML", "a <b>b</b>", "raw HTML is left out"},
		{"no alt", "![](a.png)", "image a.png has no alt text"},
		{"image elsewhere", "![alt](https://example.org/a.png)", "isn't a file next to the text"},
		{"javascript", "[x](javascript:alert(1))", `link "x" goes to javascript:alert(1)`},
		{"relative", "[x](other/)", `link "x" goes to other/`},
		{"protocol-relative", "[x](//example.org/)", "goes to //example.org/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(Parse([]byte(tt.src)).Problems(), "\n")
			if tt.want == "" && got != "" || !strings.Contains(got, tt.want) {
				t.Errorf("Problems = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInline(t *testing.T) {
	got, err := Inline("Marsh, Ann. 2020. *Archives and Their Readers*. Leiden: Brill.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Marsh, Ann. 2020. <em>Archives and Their Readers</em>. Leiden: Brill."; string(got) != want {
		t.Errorf("Inline = %q, want %q", got, want)
	}
}

func TestPlain(t *testing.T) {
	got := Plain("Marsh, Ann. 2020. *Archives and Their Readers*. [Brill](https://brill.com).")
	if want := "Marsh, Ann. 2020. Archives and Their Readers. Brill."; got != want {
		t.Errorf("Plain = %q, want %q", got, want)
	}
	// A DOI written as an autolink, as a reference style does, keeps it.
	got = Plain("Hoepman, Jaap-Henk. 2025. “Authenticity.” 5 (4). <https://doi.org/10.34669/WI.WJDS/5.4.3>.")
	if want := "Hoepman, Jaap-Henk. 2025. “Authenticity.” 5 (4). https://doi.org/10.34669/WI.WJDS/5.4.3."; got != want {
		t.Errorf("Plain = %q, want %q", got, want)
	}
}

func TestHTMLLink(t *testing.T) {
	got, err := Parse([]byte("[home](https://bw20.nl/log/quiet/) and [away](https://example.org/)")).HTML(Options{
		Link: func(dest string) string { return strings.TrimPrefix(dest, "https://bw20.nl") },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<a href="/log/quiet/">home</a>`, `<a href="https://example.org/">away</a>`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
}

func TestHeadings(t *testing.T) {
	doc := Parse([]byte("## The method\n\nText.\n\n## The method\n\n### What *we* found"))
	got := doc.Headings()
	want := []Heading{{"The method", "the-method", 2}, {"The method", "the-method-1", 2}, {"What we found", "what-we-found", 3}}
	if len(got) != len(want) {
		t.Fatalf("Headings = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("heading %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	html, err := doc.HTML(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `<h2 id="the-method">The method</h2>`) {
		t.Errorf("no id in\n%s", html)
	}
}

func TestFigure(t *testing.T) {
	figure := "<figure>\n<img src=\"bed.png\" alt=\"A bed\">\n<figcaption>A raised bed, <a href=\"https://example.org/\">in May</a>.</figcaption>\n</figure>\n"
	for _, src := range []string{
		"![A bed](bed.png)\n\n*A raised bed, [in May](https://example.org/).*", // as In the Open sends it
		"![A bed](bed.png)  \n*A raised bed, [in May](https://example.org/).*", // on the next line
		"![A bed](bed.png)\n_A raised bed, [in May](https://example.org/)._",
	} {
		got, err := Parse([]byte(src)).HTML(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != figure {
			t.Errorf("HTML(%q) =\n%s\nwant\n%s", src, got, figure)
		}
	}
	// Only an image, without italics that are all of the paragraph under it.
	for _, src := range []string{
		"![A bed](bed.png)",
		"![A bed](bed.png)\n\n*Italics* and words.",
		"![A bed](bed.png)\n\n**Bold.**",
		"![A bed](bed.png) and *words*.",
		"Words.\n*Italics.*",
	} {
		got, err := Parse([]byte(src)).HTML(Options{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "<figure>") {
			t.Errorf("HTML(%q) = %s, want no figure", src, got)
		}
	}
}
