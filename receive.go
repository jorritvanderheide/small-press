package smallpress

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/BW20/small-press/build"
	"codeberg.org/BW20/small-press/micropub"
)

// receiver is the receiver's flags, which the NixOS module sets
// (nix/module.nix, services.small-press.sites.<name>.receiver).
type receiver struct {
	listen, baseURL, pushURL, branch, author *string
	tokenFile, sshKeyFile, knownHosts, state *string
}

func receiverFlags(fl *flag.FlagSet) receiver {
	return receiver{
		listen:     fl.String("listen", "127.0.0.1:8081", "the receiver's address: host:port, unix:/path, or systemd (socket activation)"),
		baseURL:    fl.String("base-url", "", "the site's address, https://, as posts' URLs start"),
		pushURL:    fl.String("push-url", "", "the repository the receiver clones, and pushes its commits to"),
		branch:     fl.String("branch", "main", "the branch that is published"),
		author:     fl.String("author", "", "the author of the receiver's commits, as Name <address>"),
		tokenFile:  fl.String("token-file", "", "a file with the token a request has to carry"),
		sshKeyFile: fl.String("ssh-key-file", "", "the private key the receiver pushes with, over SSH"),
		knownHosts: fl.String("known-hosts", "", "the forge's SSH host keys, as a known_hosts file"),
		state:      fl.String("state", "", "the receiver's folder: its clone of the repository, and the images waiting for their post"),
	}
}

// run serves the receiver until it is stopped. A configuration that can't
// work is an error before it listens.
func (r receiver) run(st Site) error {
	var missing []string
	for name, v := range map[string]string{"-base-url": *r.baseURL, "-push-url": *r.pushURL, "-author": *r.author, "-token-file": *r.tokenFile, "-state": *r.state} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the receiver needs %s", strings.Join(missing, ", "))
	}
	token, err := os.ReadFile(*r.tokenFile)
	if err != nil {
		return fmt.Errorf("-token-file: %w", err)
	}
	if strings.TrimSpace(string(token)) == "" {
		return errors.New("-token-file: it's empty")
	}

	// git finds its configuration in HOME, which the service may not have.
	env := []string{"HOME=" + *r.state}
	if *r.sshKeyFile != "" {
		ssh := "ssh -i " + *r.sshKeyFile + " -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes"
		if *r.knownHosts != "" {
			ssh += " -o UserKnownHostsFile=" + *r.knownHosts
		}
		env = append(env, "GIT_SSH_COMMAND="+ssh)
	}
	server := &micropub.Server{
		BaseURL: strings.TrimSuffix(*r.baseURL, "/"),
		Token:   strings.TrimSpace(string(token)),
		Repo: &micropub.GitRepo{
			Dir: filepath.Join(*r.state, "repo"), URL: *r.pushURL, Branch: *r.branch, Author: *r.author, Env: env,
		},
		Check: func(fsys fs.FS) error { _, err := st.Build(fsys, st.Assets, build.Options{}); return err },
		Media: filepath.Join(*r.state, "media"),
		Now:   time.Now,
		// The file the module's path unit watches: a change to it starts the
		// deploy (nix/module.nix).
		Pushed: func() {
			stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
			if err := os.WriteFile(filepath.Join(*r.state, "pushed"), stamp, 0o600); err != nil {
				log.Printf("couldn't say it pushed: %v", err)
			}
		},
	}

	l, err := listen(*r.listen)
	if err != nil {
		return err
	}
	log.Printf("receiving on %s", l.Addr())
	// Stopping once it serves isn't a configuration that can't work: exit 1,
	// which systemd restarts after, not 2, which it doesn't.
	srv := &http.Server{Handler: server, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.Serve(l))
	return nil
}

// listen opens addr: host:port, unix:/path, or systemd, the socket systemd
// opened and handed over (sd_listen_fds).
func listen(addr string) (net.Listener, error) {
	if addr == "systemd" {
		if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
			return nil, errors.New("-listen systemd: systemd handed over no socket (LISTEN_FDS)")
		}
		return net.FileListener(os.NewFile(3, "systemd"))
	}
	if path, ok := strings.CutPrefix(addr, "unix:"); ok {
		return net.Listen("unix", path)
	}
	return net.Listen("tcp", addr)
}
