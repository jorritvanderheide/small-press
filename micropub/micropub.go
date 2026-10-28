// Package micropub is the receiver: it takes posts from the In the Open
// plugin over Micropub, with the references extension, and commits them to
// the site's repository. What it takes is the plugin's docs/micropub.md;
// what it does with it is docs/publishing.md here.
//
// Each change is checked with the site's own checks, on content/ with the
// change laid over it, before anything is committed: a post that would break
// the build is refused with the build's own messages.
package micropub

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"strings"
	"sync"
	"testing/fstest"
	"time"
)

// Repo is the site's repository, as the receiver sees it.
type Repo interface {
	// Sync brings the working copy to the remote's branch, as it is now.
	Sync() error
	// Content is content/ as it is now.
	Content() fs.FS
	// Commit writes the changes into content/, commits them, and pushes.
	Commit(message string, changes Changes) error
	// EditedElsewhere is the commit that last changed a file under content/,
	// when the receiver didn't make it, or "" when it did.
	EditedElsewhere(name string) (string, error)
}

// Changes are files under content/, by path, and what they become: nil to
// remove one.
type Changes map[string][]byte

// Server answers Micropub requests.
type Server struct {
	BaseURL string // the site's address, https://, without a slash at the end
	Token   string // what a request has to carry, as a bearer token
	Repo    Repo
	Check   func(content fs.FS) error // the site's checks, on all of content/
	Media   string                    // the folder uploads wait in
	Now     func() time.Time
	// Pushed, when set, is called after each push of a change: the module
	// starts the deploy on it, rather than wait for the deploy's timer.
	Pushed func()

	mu sync.Mutex // one request at a time changes the repository
}

// An apiError is an error as Micropub writes it.
type apiError struct {
	status      int
	code        string
	description string
}

func (e *apiError) Error() string { return e.description }

func invalid(format string, args ...any) error {
	return &apiError{http.StatusBadRequest, "invalid_request", fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &apiError{http.StatusConflict, "conflict", fmt.Sprintf(format, args...)}
}

func forbidden(format string, args ...any) error {
	return &apiError{http.StatusForbidden, "forbidden", fmt.Sprintf(format, args...)}
}

const (
	endpoint      = "/micropub"
	mediaEndpoint = "/micropub/media"
	maxPost       = 1 << 20  // a post's JSON
	maxUpload     = 20 << 20 // an image
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A staged image is fetched by its address, which is its hash: no token.
	if name, ok := strings.CutPrefix(r.URL.Path, mediaEndpoint+"/"); ok && r.Method == http.MethodGet {
		s.serveMedia(w, r, name)
		return
	}
	if !s.authorized(r) {
		writeError(w, &apiError{http.StatusUnauthorized, "unauthorized", "a request needs the site's token, as Authorization: Bearer <token>"})
		return
	}
	var err error
	switch {
	case r.URL.Path == endpoint && r.Method == http.MethodGet:
		err = s.query(w, r)
	case r.URL.Path == endpoint && r.Method == http.MethodPost:
		err = s.post(w, r)
	case r.URL.Path == mediaEndpoint && r.Method == http.MethodPost:
		err = s.upload(w, r)
	default:
		err = &apiError{http.StatusNotFound, "not_found", "no such endpoint: " + r.Method + " " + r.URL.Path}
	}
	if err != nil {
		writeError(w, err)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.Token)) == 1
}

// query answers q=config, q=category and q=source.
func (s *Server) query(w http.ResponseWriter, r *http.Request) error {
	switch q := r.URL.Query().Get("q"); q {
	case "config":
		return writeJSON(w, map[string]any{
			"media-endpoint": s.BaseURL + mediaEndpoint,
			"references":     1,
			"post-status":    []string{"published", "draft"},
		})
	case "source":
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.Repo.Sync(); err != nil {
			return err
		}
		source, err := s.source(s.Repo.Content(), r.URL.Query().Get("url"), r.URL.Query()["properties[]"])
		if err != nil {
			return err
		}
		return writeJSON(w, source)
	case "category":
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.Repo.Sync(); err != nil {
			return err
		}
		site, err := readSite(s.Repo.Content())
		if err != nil {
			return err
		}
		return writeJSON(w, map[string]any{"categories": site.ThreadNames()})
	default:
		return invalid("q=%s isn't something this site answers: it answers q=config, q=category and q=source", q)
	}
}

// post takes a create, update, delete or undelete request.
func (s *Server) post(w http.ResponseWriter, r *http.Request) error {
	if t, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); t != "application/json" {
		return invalid("a request is JSON (Content-Type: application/json)")
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, maxPost)).Decode(&req); err != nil {
		return invalid("the request isn't JSON as Micropub writes it: %v", err)
	}
	switch req.Action {
	case "":
		slug, err := s.change(func(content fs.FS) (Changes, string, string, error) { return s.create(content, req) })
		if err != nil {
			return err
		}
		w.Header().Set("Location", s.BaseURL+"/log/"+slug+"/")
		w.WriteHeader(http.StatusAccepted)
		return nil
	case "update", "delete", "undelete":
		_, err := s.change(func(content fs.FS) (Changes, string, string, error) { return s.edit(content, req) })
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	default:
		return invalid("action %q isn't one this site takes: update, delete or undelete", req.Action)
	}
}

// change makes a change to the repository: from its newest commit, checked,
// committed and pushed. A push that is refused, because something else was
// pushed in between, is made again once, on top of that.
func (s *Server) change(make func(content fs.FS) (Changes, string, string, error)) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last error
	for range 2 {
		if err := s.Repo.Sync(); err != nil {
			return "", err
		}
		content := s.Repo.Content()
		changes, message, slug, err := make(content)
		if err != nil {
			return "", err
		}
		laid, err := overlay(content, changes)
		if err != nil {
			return "", err
		}
		if err := s.Check(laid); err != nil {
			return "", invalid("%v", err)
		}
		if last = s.Repo.Commit(message, changes); last == nil {
			if s.Pushed != nil {
				s.Pushed()
			}
			return slug, nil
		}
		log.Printf("%s: %v; trying again", message, last)
	}
	return "", fmt.Errorf("couldn't push: %w", last)
}

// overlay is content with the changes laid over it, in memory.
func overlay(content fs.FS, changes Changes) (fs.FS, error) {
	laid := fstest.MapFS{}
	err := fs.WalkDir(content, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(content, name)
		laid[name] = &fstest.MapFile{Data: data}
		return err
	})
	if err != nil {
		return nil, err
	}
	for name, data := range changes {
		if data == nil {
			delete(laid, name)
		} else {
			laid[name] = &fstest.MapFile{Data: data}
		}
	}
	return laid, nil
}

func writeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	var e *apiError
	if !errors.As(err, &e) {
		// The repository or the disk, not the request: logged in full, and
		// said in a sentence.
		log.Print(err)
		e = &apiError{http.StatusInternalServerError, "server_error", "the site couldn't take it: " + err.Error()}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	json.NewEncoder(w).Encode(map[string]string{"error": e.code, "error_description": e.description})
}
