# Sites

A site on small-press is a repository of its own: a small Go program, its
pages, and its content. The program is the library's `Main` with the site's
pages; everything but the pages is the library's
([The library and a site](architecture.md#the-library-and-a-site)).

## What a site's repository holds

| Path | What it holds |
| --- | --- |
| `main.go` | `smallpress.Main`, with the site's pages, its embedded `web/`, and its post page's section ids. |
| `pages.go` | Which pages the site has, and what each template gets. |
| `pages_test.go` | The pages' tests, on an in-memory site. |
| `web/` | Templates and static files, embedded with `embed.FS`. Everything in `web/static/` is published, each at an address with its hash (`asset "site.css"` in a template). |
| `content/` | Posts and pages, in the [format](architecture.md#content) every site shares. |
| `go.mod` | The library, at the commit the site builds with. |
| `flake.nix`, `nix/` | `program` and `site`, which the deploy builds. |
| `check.sh`, `.forgejo/` | The checks, as the library's. |

The flake has two packages the module relies on: `site`, the built site,
and `program`, whose `bin/` has the program the module's `program` option
names. The receiver runs that program, so it checks a post against this
site's pages, as the deploy will build it.

## Starting one

1. Copy `example/` (the Go files, `web/` and `content/`) into a new
   repository, and `flake.nix`, `nix/program.nix`, `nix/site.nix`,
   `check.sh` and `.forgejo/` from this one, or from a site already
   running.
2. `go mod init codeberg.org/<owner>/<site>`, change the import of `web` in
   `main.go` and `pages_test.go` to it, then
   `go get codeberg.org/BW20/small-press@main` and `go mod tidy`.
3. In `nix/program.nix`: the program's name, its files (no `build/` or
   `example/` here), and `vendorHash = lib.fakeHash`. `nix build .#program`
   then says what the hash is.
4. Change what you like: `content/site.toml` first, then `web/` and
   `pages.go`. `nix run .#dev` serves it while you do.
5. Add the site to the host: `services.small-press.sites.<name>`, with its
   domain, its repository, the program's name, and for the receiver a token
   and a deploy key with write access to that repository only.

A site whose pages are written by hand, not only posts from In the Open,
lets CI check pushes that only change `content/` too: the receiver checks
the commits it makes, but nothing checks a hand-made one before the deploy
does, and the deploy only alerts whoever reads the host's status page.

## Working on the library and a site at once

A `go.work` next to the site's `go.mod`, ignored by its `.gitignore`,
builds it with the library's working copy:

```
go 1.26.8

use (
	.
	../small-press
)
```

Nix doesn't see it: `nix build` takes the library at the commit `go.mod`
pins.

## Moving a site to a new commit of the library

1. Push the library's change.
2. In the site: `GOWORK=off go get codeberg.org/BW20/small-press@main` and
   `GOWORK=off go mod tidy`.
3. Set `vendorHash` to what `nix build .#program` says, and run
   `./check.sh` and `nix flake check`.

A change to the library that a site's pages don't compile against shows
here, as a compile error. Each site moves when it is ready: the library
has no stable API, and two sites may run two commits of it.

The host's module comes from the host's own pin of this flake. It runs each
site's program with flags, so a change to the receiver's flags needs the
module and every site moved together.
