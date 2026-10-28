package micropub

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/mail"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// GitRepo is the site's repository, cloned into Dir: git does the work, with
// the environment in Env (such as the SSH key it pushes with).
type GitRepo struct {
	Dir    string
	URL    string // where it is fetched from and pushed to
	Branch string
	Author string // Name <address>, the commits' author and committer
	Env    []string
}

func (g *GitRepo) git(args ...string) error {
	_, err := g.output(args...)
	return err
}

// output runs git, and is what it wrote, or an error with what it said.
func (g *GitRepo) output(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", g.Dir}, args...)...)
	cmd.Env = append(os.Environ(), g.Env...)
	var out, said bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &said
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(said.String()+out.String()))
	}
	return out.String(), nil
}

// EditedElsewhere is the commit that last changed a file under content/,
// when someone other than the receiver made it, or "" when the receiver did,
// or the file has no history yet.
func (g *GitRepo) EditedElsewhere(name string) (string, error) {
	author, err := mail.ParseAddress(g.Author)
	if err != nil {
		return "", fmt.Errorf("the author %q isn't Name <address>: %w", g.Author, err)
	}
	out, err := g.output("log", "-1", "--format=%h %ae", "--", path.Join("content", name))
	if err != nil {
		return "", err
	}
	commit, email, _ := strings.Cut(strings.TrimSpace(out), " ")
	if commit == "" || strings.EqualFold(email, author.Address) {
		return "", nil
	}
	return commit, nil
}

// Sync clones the repository the first time, and after that fetches the
// branch and resets to it, so every change starts from what was pushed.
func (g *GitRepo) Sync() error {
	if _, err := os.Stat(filepath.Join(g.Dir, ".git")); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(g.Dir, 0o700); err != nil {
			return err
		}
		return g.git("clone", "--quiet", "--branch", g.Branch, "--single-branch", g.URL, ".")
	}
	if err := g.git("fetch", "--quiet", "origin", g.Branch); err != nil {
		return err
	}
	if err := g.git("reset", "--quiet", "--hard", "FETCH_HEAD"); err != nil {
		return err
	}
	return g.git("clean", "--quiet", "-fdx")
}

// Content is content/ in the working copy.
func (g *GitRepo) Content() fs.FS { return os.DirFS(filepath.Join(g.Dir, "content")) }

// Commit writes the changes, commits them, and pushes.
func (g *GitRepo) Commit(message string, changes Changes) error {
	author, err := mail.ParseAddress(g.Author)
	if err != nil {
		return fmt.Errorf("the author %q isn't Name <address>: %w", g.Author, err)
	}
	for name, data := range changes {
		// Only ever under content/: the names come from requests.
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%s isn't a path under content/", name)
		}
		file := filepath.Join(g.Dir, "content", filepath.FromSlash(name))
		if data == nil {
			if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return err
		}
	}
	if err := g.git("add", "--all", "--", "content"); err != nil {
		return err
	}
	identity := []string{"-c", "user.name=" + author.Name, "-c", "user.email=" + author.Address}
	if err := g.git(append(identity, "commit", "--quiet", "-m", message)...); err != nil {
		return err
	}
	return g.git("push", "--quiet", "origin", "HEAD:refs/heads/"+g.Branch)
}
