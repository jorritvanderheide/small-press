package micropub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// An image waits in the media folder this long for a post to take it.
const keep = 24 * time.Hour

// A staged image's name: its hash, and its extension.
var stagedName = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg)$`)

// upload takes an image, a PNG or a JPEG, and keeps it until a post takes
// it, by its hash.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<16)
	file, _, err := r.FormFile("file")
	if err != nil {
		return invalid("an upload is multipart/form-data with the image in a field called file, at most 20 MB")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil {
		return err
	}
	if len(data) > maxUpload {
		return &apiError{http.StatusRequestEntityTooLarge, "invalid_request", "an image is at most 20 MB"}
	}
	var ext string
	switch http.DetectContentType(data) {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	default:
		return &apiError{http.StatusUnsupportedMediaType, "invalid_request", "an image is a PNG or a JPEG"}
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + ext
	if err := os.MkdirAll(s.Media, 0o700); err != nil {
		return err
	}
	s.forget()
	if err := os.WriteFile(filepath.Join(s.Media, name), data, 0o600); err != nil {
		return err
	}
	w.Header().Set("Location", s.BaseURL+mediaEndpoint+"/"+name)
	w.WriteHeader(http.StatusCreated)
	return nil
}

// serveMedia serves a staged image, so the plugin can show it before the
// post is up.
func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, name string) {
	if !stagedName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.Media, name))
}

// staged is the image the media endpoint holds by that name.
func (s *Server) staged(name string) ([]byte, error) {
	if !stagedName.MatchString(name) {
		return nil, invalid("image %s isn't one the media endpoint gave", name)
	}
	data, err := os.ReadFile(filepath.Join(s.Media, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, invalid("image %s isn't in the media endpoint: an upload waits a day for its post, so upload it again", name)
	}
	return data, err
}

// forget removes the images no post took in time.
func (s *Server) forget() {
	entries, _ := os.ReadDir(s.Media)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && s.Now().Sub(info.ModTime()) > keep {
			os.Remove(filepath.Join(s.Media, e.Name()))
		}
	}
}
