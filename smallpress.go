// Package smallpress makes a site from content/, as a site's own program
// asks it to: Main is that program's main. It writes the site to a folder
// (-out), as the Nix build does, or serves it while you write (-serve), built
// again on every request. With -receive it is the receiver, which takes posts
// over Micropub and commits them (receive.go).
//
// Reading content/ is in package content, putting it together and checking
// it in site, Markdown in render, and every file but the pages in build. The
// pages are the site's own: Site.Pages.
package smallpress

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"

	"codeberg.org/BW20/small-press/build"
	"codeberg.org/BW20/small-press/content"
	"codeberg.org/BW20/small-press/site"
)

// A Site is what a site's own program gives Main: how its pages look.
type Site struct {
	// Pages writes the site's pages, from its templates and static files in
	// assets: one at every address the site gives something (build.Verify),
	// and the static files (build.Static). Every other file is the
	// library's (build.Common).
	Pages func(s *site.Site, assets fs.FS, o build.Options) (build.Files, error)
	// Assets are the templates and static files, embedded in the program.
	Assets fs.FS
	// Options are what the site's checks need to know about its pages.
	Options site.Options
}

// Build is the whole site from content: read, checked, and every file
// written into memory, the site's pages and the library's.
func (st Site) Build(contentFS, assets fs.FS, o build.Options) (build.Files, error) {
	c, err := content.Read(contentFS)
	if err != nil {
		return nil, err
	}
	s, err := site.New(c, st.Options)
	if err != nil {
		return nil, err
	}
	common, err := build.Common(s)
	pages, perr := st.Pages(s, assets, o)
	if err := errors.Join(err, perr); err != nil {
		return nil, err
	}
	if err := build.Verify(s, pages, common); err != nil {
		return nil, err
	}
	maps.Copy(pages, common)
	return pages, nil
}

// Main is a site's program: its flags, and the three ways to run.
func Main(st Site) {
	fl := flag.NewFlagSet(path.Base(os.Args[0]), flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprintf(fl.Output(), "Usage: %s -out <folder> | -serve <host:port> | -receive [flags]\n\nFlags:\n", fl.Name())
		fl.PrintDefaults()
	}
	var (
		contentDir = fl.String("content", "content", "the folder with the posts and pages")
		out        = fl.String("out", "", "write the site to this folder, which mustn't exist yet or must be empty")
		serve      = fl.String("serve", "", "serve the site on this address, such as 127.0.0.1:8080, built again on every request (development)")
		devAssets  = fl.String("dev-assets", "", "read the templates and static files from web/ in this folder instead of the program, so edits show at once (development)")
		buildNum   = fl.Int("build", 0, "the build number at the foot of every page, the commit count; left out when 0")
		receive    = fl.Bool("receive", false, "be the receiver: take posts over Micropub, and commit them (the -listen, -base-url, -push-url, -branch, -author, -token-file, -ssh-key-file, -known-hosts and -state flags)")
		r          = receiverFlags(fl)
	)
	fl.Parse(os.Args[1:])
	// A wrong invocation exits with 2, as a wrong flag does.
	if *receive {
		if err := r.run(st); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if (*out == "") == (*serve == "") {
		fmt.Fprintln(os.Stderr, "give one of -out, -serve and -receive")
		fl.Usage()
		os.Exit(2)
	}

	assets := st.Assets
	if *devAssets != "" {
		sub, err := fs.Sub(os.DirFS(*devAssets), "web")
		if err != nil {
			log.Fatal(err)
		}
		assets = sub
	}
	make := func() (build.Files, error) {
		return st.Build(os.DirFS(*contentDir), assets, build.Options{Build: *buildNum})
	}

	if *out != "" {
		files, err := make()
		if err != nil {
			log.Fatal(err)
		}
		if err := build.Write(*out, files); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Printf("serving on http://%s", *serve)
	log.Fatal(http.ListenAndServe(*serve, devHandler(make)))
}

// devHandler builds the site on every request and serves the file asked
// for, or the build's error, so a change to a post or a template shows on
// reload.
func devHandler(site func() (build.Files, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		files, err := site()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		data, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		kind := mime.TypeByExtension(path.Ext(name))
		if kind == "" {
			kind = "text/plain; charset=utf-8" // cite.bib, the nginx includes
		}
		w.Header().Set("Content-Type", kind)
		w.Write(data)
	})
}
