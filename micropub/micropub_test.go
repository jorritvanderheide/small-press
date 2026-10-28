package micropub

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"codeberg.org/BW20/small-press/build"
	"codeberg.org/BW20/small-press/content"
	"codeberg.org/BW20/small-press/site"
)

const base = "https://bw20.test"

var now = time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)

// memRepo is a repository in memory: what was committed, and how.
type memRepo struct {
	files    fstest.MapFS
	messages []string
	refuse   int               // pushes to refuse first, as when something else was pushed
	edited   map[string]string // files last changed by someone else, and the commit
}

func (m *memRepo) EditedElsewhere(name string) (string, error) { return m.edited[name], nil }

func (m *memRepo) Sync() error    { return nil }
func (m *memRepo) Content() fs.FS { return m.files }
func (m *memRepo) Commit(message string, changes Changes) error {
	if m.refuse > 0 {
		m.refuse--
		return errors.New("rejected: fetch first")
	}
	for name, data := range changes {
		delete(m.edited, name)
		if data == nil {
			delete(m.files, name)
		} else {
			m.files[name] = &fstest.MapFile{Data: data}
		}
	}
	m.messages = append(m.messages, message)
	return nil
}

// check is the library's checks, as every site's program runs them: the
// site's pages are its own, and tested there.
func check(fsys fs.FS) error {
	c, err := content.Read(fsys)
	if err != nil {
		return err
	}
	s, err := site.New(c, site.Options{SectionIDs: []string{"references"}})
	if err != nil {
		return err
	}
	_, err = build.Common(s)
	return err
}

func setup(t *testing.T) (*Server, *memRepo) {
	repo := &memRepo{files: fstest.MapFS{
		"site.toml":         {Data: []byte("title = 'BW20'\nurl = 'https://bw20.test'\ndescription = 'A site.'\n[author]\ngiven = 'Jorrit'\nfamily = 'van der Heide'\n")},
		"threads.toml":      {Data: []byte("[[threads]]\nname = 'tech'\ndescription = 'Tech.'\n")},
		"log/hand/index.md": {Data: []byte("+++\ntitle = 'By hand'\nsummary = 'Written by hand.'\ndate = 2026-01-01T10:00:00Z\n+++\n\nWords.\n")},
	}}
	s := &Server{BaseURL: base, Token: "secret", Repo: repo, Check: check, Media: t.TempDir(), Now: func() time.Time { return now }}
	return s, repo
}

func do(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func expect(t *testing.T, w *httptest.ResponseRecorder, status int, says ...string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body)
	}
	for _, s := range says {
		if !strings.Contains(w.Body.String(), s) {
			t.Errorf("response doesn't say %q: %s", s, w.Body)
		}
	}
}

func file(t *testing.T, repo *memRepo, name string, says ...string) {
	t.Helper()
	f, ok := repo.files[name]
	if !ok {
		t.Fatalf("no %s", name)
	}
	for _, s := range says {
		if !strings.Contains(string(f.Data), s) {
			t.Errorf("%s doesn't say %q:\n%s", name, s, f.Data)
		}
	}
}

func upload(t *testing.T, s *Server) string {
	t.Helper()
	var img bytes.Buffer
	png.Encode(&img, image.NewGray(image.Rect(0, 0, 20, 10)))
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "bed.png")
	fw.Write(img.Bytes())
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/micropub/media", &body)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	expect(t, w, http.StatusCreated)
	return w.Header().Get("Location")
}

func create(props map[string]any) string {
	p := map[string][]any{}
	for k, v := range props {
		if list, ok := v.([]any); ok {
			p[k] = list
		} else {
			p[k] = []any{v}
		}
	}
	b, _ := json.Marshal(map[string]any{"type": []string{"h-entry"}, "properties": p})
	return string(b)
}

func quiet(image string) map[string]any {
	return map[string]any{
		"name":     "The NixOS module system is a quiet revolution",
		"summary":  "Why it matters.",
		"content":  "As [(Marsh 2020, 12)](#ref-marsh2020) says, and [by hand](" + base + "/log/hand/).\n\n![A raised bed](" + image + ")\n",
		"category": []any{"tech"},
		"mp-slug":  "quiet",
		"references": []any{map[string]any{
			"key": "marsh2020", "text": "Marsh, Ann. 2020. *Archives*.", "url": "https://doi.org/10.1/x",
		}},
	}
}

func TestToken(t *testing.T) {
	s, _ := setup(t)
	for _, header := range []string{"", "Bearer wrong", "secret"} {
		r := httptest.NewRequest(http.MethodGet, "/micropub?q=config", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		expect(t, w, http.StatusUnauthorized, `"error":"unauthorized"`)
	}
}

func TestQuery(t *testing.T) {
	s, _ := setup(t)
	expect(t, do(s, http.MethodGet, "/micropub?q=config", ""), http.StatusOK, `"media-endpoint":"https://bw20.test/micropub/media"`, `"references":1`)
	expect(t, do(s, http.MethodGet, "/micropub?q=category", ""), http.StatusOK, `"categories":["tech"]`)
	expect(t, do(s, http.MethodGet, "/micropub?q=config", ""), http.StatusOK, `"post-status":["published","draft"]`)
	expect(t, do(s, http.MethodGet, "/micropub?q=source&properties[]=post-status&url="+base+"/log/hand/", ""), http.StatusOK, `{"properties":{"post-status":["published"]}}`)
	expect(t, do(s, http.MethodGet, "/micropub?q=source&url="+base+"/log/hand/", ""), http.StatusOK,
		`"type":["h-entry"]`, `"name":["By hand"]`, `"summary":["Written by hand."]`, `"content":["Words."]`, `"category":[]`, `"references":[]`)
	expect(t, do(s, http.MethodGet, "/micropub?q=source&url="+base+"/log/nope/", ""), http.StatusBadRequest, "there's no post at")
	expect(t, do(s, http.MethodGet, "/micropub?q=nope", ""), http.StatusBadRequest, "q=nope isn't something this site answers")
}

func TestCreate(t *testing.T) {
	s, repo := setup(t)
	image := upload(t, s)
	w := do(s, http.MethodPost, "/micropub", create(quiet(image)))
	expect(t, w, http.StatusAccepted)
	if got := w.Header().Get("Location"); got != base+"/log/quiet/" {
		t.Errorf("Location = %q", got)
	}
	if strings.Join(repo.messages, "; ") != "publish: quiet" {
		t.Errorf("commits = %v", repo.messages)
	}
	staged := strings.TrimPrefix(image, base+"/micropub/media/")
	file(t, repo, "log/quiet/index.md",
		"title = \"The NixOS module system is a quiet revolution\"",
		"date = 2026-10-28T09:30:00Z",
		"threads = [\"tech\"]",
		"generator = \"micropub\"",
		"references = [\"marsh2020\"]",
		"[(Marsh 2020, 12)](#ref-marsh2020)",
		"[by hand](https://bw20.test/log/hand/)",
		"![A raised bed]("+staged[:12]+".png)",
	)
	file(t, repo, "log/quiet/"+staged[:12]+".png")
	file(t, repo, "works/marsh2020.toml", `text = "Marsh, Ann. 2020. *Archives*."`, `url = "https://doi.org/10.1/x"`)
	// And the result builds.
	if err := check(repo.files); err != nil {
		t.Error(err)
	}
}

func TestCreateRefused(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]any)
		says   string
	}{
		{"no summary", func(p map[string]any) { delete(p, "summary") }, "log/quiet: index.md: missing summary"},
		{"a thread that isn't a name", func(p map[string]any) { p["category"] = []any{"Cooking"} }, `thread \"Cooking\" isn't a name`},
		{"citation without reference", func(p map[string]any) { delete(p, "references") }, `citation \"(Marsh 2020, 12)\" of marsh2020 has no reference`},
		{"slug taken", func(p map[string]any) { p["mp-slug"] = "hand" }, "slug hand is taken"},
		{"bad slug", func(p map[string]any) { p["mp-slug"] = "../hand" }, "is lowercase letters"},
		{"bad key", func(p map[string]any) {
			p["references"] = []any{map[string]any{"key": "../x", "text": "X."}}
		}, "isn't a citation key"},
		{"HTML", func(p map[string]any) { p["content"] = map[string]any{"html": "<p>x</p>"} }, "content is a plain string of Markdown"},
		{"no title", func(p map[string]any) { delete(p, "name") }, "a post needs a name"},
		{"image not uploaded", func(p map[string]any) {
			p["content"] = "![x](" + base + "/micropub/media/" + strings.Repeat("a", 64) + ".png) [(Marsh 2020, 12)](#ref-marsh2020)"
		}, "isn't in the media endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, repo := setup(t)
			p := quiet(upload(t, s))
			tt.change(p)
			expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusBadRequest, `"error":"invalid_request"`, tt.says)
			if len(repo.messages) > 0 {
				t.Errorf("committed %v", repo.messages)
			}
		})
	}
}

func TestSlugOf(t *testing.T) {
	if got := slugOf("The NixOS module system: a quiet revolution!"); got != "the-nixos-module-system-a-quiet-revolution" {
		t.Errorf("slugOf = %q", got)
	}
}

func TestEdit(t *testing.T) {
	s, repo := setup(t)
	expect(t, do(s, http.MethodPost, "/micropub", create(quiet(upload(t, s)))), http.StatusAccepted)

	update := `{"action": "update", "url": "` + base + `/log/quiet/", "replace": {
		"content": ["Now without the bed, as [(Marsh 2020)](#ref-marsh2020) says."],
		"updated": ["2026-11-01T08:00:00Z"]}}`
	expect(t, do(s, http.MethodPost, "/micropub", update), http.StatusNoContent)
	file(t, repo, "log/quiet/index.md", "date = 2026-10-28T09:30:00Z", "updated = 2026-11-01T08:00:00Z", "Now without the bed")
	for name := range repo.files {
		if strings.HasSuffix(name, ".png") {
			t.Errorf("%s stayed, though the post no longer has it", name)
		}
	}

	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/quiet/", "reason": " The figures\n were wrong. "}`), http.StatusNoContent)
	file(t, repo, "log/quiet/index.md", "withdrawn = 2026-10-28T09:30:00Z", `withdrawn-reason = "The figures were wrong."`)
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "undelete", "url": "`+base+`/log/quiet/"}`), http.StatusNoContent)
	if strings.Contains(string(repo.files["log/quiet/index.md"].Data), "withdrawn") {
		t.Error("still withdrawn")
	}
	if got := strings.Join(repo.messages, "; "); got != "publish: quiet; update: quiet; withdraw: quiet; restore: quiet" {
		t.Errorf("commits = %s", got)
	}

	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/hand/"}`), http.StatusForbidden, "written by hand")
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/nope/"}`), http.StatusBadRequest, "there's no post at")
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "update", "url": "`+base+`/log/quiet/", "replace": {"mp-slug": ["other"]}}`), http.StatusBadRequest, "mp-slug can't be replaced")
}

// A push refused because something else was pushed in between is made
// again, on top of it.
func TestRetry(t *testing.T) {
	s, repo := setup(t)
	repo.refuse = 1
	expect(t, do(s, http.MethodPost, "/micropub", create(quiet(upload(t, s)))), http.StatusAccepted)
	repo.refuse = 2
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/quiet/"}`), http.StatusInternalServerError, "couldn't push")
}

func TestMedia(t *testing.T) {
	s, _ := setup(t)
	location := upload(t, s)
	r := httptest.NewRequest(http.MethodGet, strings.TrimPrefix(location, base), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	expect(t, w, http.StatusOK)
	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "notes.txt")
	fw.Write([]byte("not an image"))
	mw.Close()
	r = httptest.NewRequest(http.MethodPost, "/micropub/media", &body)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	expect(t, w, http.StatusUnsupportedMediaType, "a PNG or a JPEG")
}

// GitRepo with a real git, against a repository on disk.
func TestGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	remote := filepath.Join(dir, "remote.git")
	seed := filepath.Join(dir, "seed")
	run("init", "-q", "--bare", "-b", "main", remote)
	run("init", "-q", "-b", "main", seed)
	os.MkdirAll(filepath.Join(seed, "content"), 0o755)
	os.WriteFile(filepath.Join(seed, "content", "site.toml"), []byte("title = 'BW20'\n"), 0o644)
	run("-C", seed, "add", ".")
	run("-C", seed, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "seed")
	run("-C", seed, "push", "-q", remote, "main")

	g := &GitRepo{Dir: filepath.Join(dir, "clone"), URL: remote, Branch: "main", Author: "Receiver <receiver@bw20.test>",
		Env: []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}}
	if err := g.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(g.Content(), "site.toml"); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("publish: quiet", Changes{"log/quiet/index.md": []byte("text"), "site.toml": nil}); err != nil {
		t.Fatal(err)
	}
	if got := run("--git-dir", remote, "log", "-1", "--format=%s %an <%ae>"); got != "publish: quiet Receiver <receiver@bw20.test>\n" {
		t.Errorf("pushed %q", got)
	}
	if got := run("--git-dir", remote, "show", "main:content/log/quiet/index.md"); got != "text" {
		t.Errorf("content %q", got)
	}
	if err := g.Commit("escape", Changes{"../flake.nix": []byte("x")}); err == nil {
		t.Error("wrote outside content/")
	}
	// Another push in between: Sync takes it.
	run("-C", seed, "pull", "-q", remote, "main")
	os.WriteFile(filepath.Join(seed, "content", "threads.toml"), []byte(""), 0o644)
	run("-C", seed, "add", ".")
	run("-C", seed, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "by hand")
	run("-C", seed, "push", "-q", remote, "main")
	if err := g.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Stat(g.Content(), "threads.toml"); err != nil {
		t.Error("Sync didn't take the other push")
	}

	// Who last changed a post: the receiver, then someone else.
	if commit, err := g.EditedElsewhere("log/quiet/index.md"); err != nil || commit != "" {
		t.Errorf("after the receiver: %q, %v", commit, err)
	}
	os.WriteFile(filepath.Join(seed, "content", "log", "quiet", "index.md"), []byte("fixed by hand"), 0o644)
	run("-C", seed, "add", ".")
	run("-C", seed, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "typo")
	run("-C", seed, "push", "-q", remote, "main")
	if err := g.Sync(); err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(run("--git-dir", remote, "log", "-1", "--format=%h"))
	if commit, err := g.EditedElsewhere("log/quiet/index.md"); err != nil || commit != want {
		t.Errorf("after a hand edit: %q, %v, want %s", commit, err, want)
	}
	if commit, _ := g.EditedElsewhere("log/none/index.md"); commit != "" {
		t.Errorf("a file with no history: %q", commit)
	}
}

// Pushed is called once for each change pushed, and not for one refused or
// one that couldn't be pushed.
func TestPushed(t *testing.T) {
	s, repo := setup(t)
	pushed := 0
	s.Pushed = func() { pushed++ }
	p := quiet(upload(t, s))
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusAccepted)
	if pushed != 1 {
		t.Errorf("pushed %d times for a post, want once", pushed)
	}
	delete(p, "summary")
	p["mp-slug"] = "other"
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusBadRequest)
	repo.refuse = 2
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/quiet/"}`), http.StatusInternalServerError)
	if pushed != 1 {
		t.Errorf("pushed %d times, want once: a refused post and a failed push aren't pushes", pushed)
	}
}

// An image address pandoc wrote in <…> is taken as any other.
func TestImageInAngleBrackets(t *testing.T) {
	s, repo := setup(t)
	image := upload(t, s)
	p := quiet(image)
	p["content"] = "As [(Marsh 2020, 12)](#ref-marsh2020) says.\n\n![A raised bed](<" + image + ">)\n"
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusAccepted)
	staged := strings.TrimPrefix(image, base+"/micropub/media/")
	file(t, repo, "log/quiet/index.md", "![A raised bed]("+staged[:12]+".png)")
}

// An update keeps a DOI given by hand: the receiver takes none from a request.
func TestUpdateKeepsDOI(t *testing.T) {
	s, repo := setup(t)
	expect(t, do(s, http.MethodPost, "/micropub", create(quiet(upload(t, s)))), http.StatusAccepted)
	index := repo.files["log/quiet/index.md"]
	index.Data = []byte(strings.Replace(string(index.Data), "generator = ", "doi = \"10.64000/abcd-1234\"\ngenerator = ", 1))
	update := `{"action": "update", "url": "` + base + `/log/quiet/", "replace": {"content": ["Changed, as [(Marsh 2020)](#ref-marsh2020) says."]}}`
	expect(t, do(s, http.MethodPost, "/micropub", update), http.StatusNoContent)
	file(t, repo, "log/quiet/index.md", `doi = "10.64000/abcd-1234"`, "Changed, as")
}

// A draft is sent again while it is one, published once, and discarded
// rather than withdrawn.
func TestDraft(t *testing.T) {
	s, repo := setup(t)
	p := quiet(upload(t, s))
	p["post-status"] = "draft"
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusAccepted)
	file(t, repo, "log/quiet/index.md", "draft = true", "date = 2026-10-28T09:30:00Z")
	source := "/micropub?q=source&properties[]=post-status&url=" + base + "/log/quiet/"
	expect(t, do(s, http.MethodGet, source, ""), http.StatusOK, `"post-status":["draft"]`)

	again := `{"action": "update", "url": "` + base + `/log/quiet/", "replace": {
		"post-status": ["draft"], "content": ["Still a draft, as [(Marsh 2020)](#ref-marsh2020) says."]}}`
	expect(t, do(s, http.MethodPost, "/micropub", again), http.StatusNoContent)
	file(t, repo, "log/quiet/index.md", "draft = true", "Still a draft")

	// Published two days on: that is its date.
	s.Now = func() time.Time { return now.Add(48 * time.Hour) }
	publish := `{"action": "update", "url": "` + base + `/log/quiet/", "replace": {
		"post-status": ["published"], "content": ["Out now, as [(Marsh 2020)](#ref-marsh2020) says."], "updated": ["2026-10-29T08:00:00Z"]}}`
	expect(t, do(s, http.MethodPost, "/micropub", publish), http.StatusNoContent)
	index := string(repo.files["log/quiet/index.md"].Data)
	if strings.Contains(index, "draft") || strings.Contains(index, "updated") || !strings.Contains(index, "date = 2026-10-30T09:30:00Z") {
		t.Errorf("published as:\n%s", index)
	}
	expect(t, do(s, http.MethodGet, source, ""), http.StatusOK, `"post-status":["published"]`)
	expect(t, do(s, http.MethodPost, "/micropub", again), http.StatusBadRequest, "doesn't become a draft again")

	p = quiet(upload(t, s))
	p["mp-slug"], p["post-status"] = "second", "draft"
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusAccepted)
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "undelete", "url": "`+base+`/log/second/"}`), http.StatusBadRequest, "nothing to restore")
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/second/"}`), http.StatusNoContent)
	for name := range repo.files {
		if strings.HasPrefix(name, "log/second/") {
			t.Errorf("%s stayed after the draft was discarded", name)
		}
	}
	if got := strings.Join(repo.messages, "; "); got != "draft: quiet; update: quiet; publish: quiet; draft: second; discard: second" {
		t.Errorf("commits = %s", got)
	}

	p["post-status"] = "private"
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusBadRequest, "post-status is published or draft")
}

// The whole post comes back as the site keeps it: what In the Open compares
// a note with before an update.
func TestSource(t *testing.T) {
	s, _ := setup(t)
	image := upload(t, s)
	expect(t, do(s, http.MethodPost, "/micropub", create(quiet(image))), http.StatusAccepted)
	staged := strings.TrimPrefix(image, base+"/micropub/media/")
	expect(t, do(s, http.MethodGet, "/micropub?q=source&url="+base+"/log/quiet/", ""), http.StatusOK,
		`"name":["The NixOS module system is a quiet revolution"]`,
		`"category":["tech"]`,
		`"references":[{"key":"marsh2020","text":"Marsh, Ann. 2020. *Archives*.","url":"https://doi.org/10.1/x"}]`,
		`![A raised bed](`+staged[:12]+`.png)`,
		`"post-status":["published"]`,
	)
	expect(t, do(s, http.MethodGet, "/micropub?q=source&properties[]=name&properties[]=nope&url="+base+"/log/quiet/", ""), http.StatusOK,
		`{"properties":{"name":["The NixOS module system is a quiet revolution"]}}`)
}

// A post edited elsewhere since the receiver wrote it isn't replaced without
// being asked; withdrawing it doesn't replace its text, so isn't refused.
func TestEditedElsewhere(t *testing.T) {
	s, repo := setup(t)
	expect(t, do(s, http.MethodPost, "/micropub", create(quiet(upload(t, s)))), http.StatusAccepted)
	repo.edited = map[string]string{"log/quiet/index.md": "3f2a1b9"}
	update := func(overwrite string) string {
		return `{"action": "update", "url": "` + base + `/log/quiet/",` + overwrite + ` "replace": {"content": ["Again, as [(Marsh 2020)](#ref-marsh2020) says."]}}`
	}
	expect(t, do(s, http.MethodPost, "/micropub", update("")), http.StatusConflict, `"error":"conflict"`, "log/quiet was edited elsewhere since it was last published (commit 3f2a1b9)")
	expect(t, do(s, http.MethodPost, "/micropub", update(` "mp-overwrite": true,`)), http.StatusNoContent)
	file(t, repo, "log/quiet/index.md", "Again, as")
	// Written by the receiver again, so the next update goes through.
	expect(t, do(s, http.MethodPost, "/micropub", update("")), http.StatusNoContent)

	repo.edited = map[string]string{"log/quiet/index.md": "4a5b6c7"}
	expect(t, do(s, http.MethodPost, "/micropub", `{"action": "delete", "url": "`+base+`/log/quiet/"}`), http.StatusNoContent)
}

// A category a post names becomes one of the site's, and q=category says so.
func TestNewCategory(t *testing.T) {
	s, repo := setup(t)
	p := quiet(upload(t, s))
	p["category"] = []any{"tech", "open-science"}
	expect(t, do(s, http.MethodPost, "/micropub", create(p)), http.StatusAccepted)
	file(t, repo, "log/quiet/index.md", `threads = ["tech", "open-science"]`)
	expect(t, do(s, http.MethodGet, "/micropub?q=category", ""), http.StatusOK, `"categories":["tech","open-science"]`)
}
