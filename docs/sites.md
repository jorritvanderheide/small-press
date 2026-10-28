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
| `go.mod` | The library, at the release the site builds with. |
| `flake.nix`, `nix/` | `program` and `site`, which the deploy builds (`nix/package.nix`, `nix/site.nix`). |
| `.gitignore` | `go.work` and `go.work.sum`, for working on the library and the site at once. |
| `check.sh`, `.forgejo/` | The checks, as the library's. |

The flake has two packages the module relies on: `site`, the built site,
and `program`, whose `bin/` has the program the module's `program` option
names. The receiver runs that program, so it checks a post against this
site's pages, as the deploy will build it.

## Starting one

1. Copy `example/` (the Go files, `web/` and `content/`) into a new repository,
   and `flake.nix`, `nix/package.nix`, `nix/site.nix`, `check.sh`,
   `.forgejo/` and `.gitignore` from a site already running, such as
   `codeberg.org/BW20/blog-jeltje`. Copied from this repository instead,
   the flake loses the module, its VM test and `git` in the dev shell,
   `dev` runs `go run . -serve … -dev-assets .`, `nix/program.nix` becomes
   `nix/package.nix` without `git` for its tests, and `nix/site.nix` reads
   `../content` rather than `../example/content`.
2. `go mod init codeberg.org/<owner>/<site>`, change the import of `web` in
   `main.go` and `pages_test.go` to it, then
   `go get codeberg.org/BW20/small-press@latest` and `go mod tidy`.
3. In `nix/package.nix`: the program's name, and `vendorHash =
   lib.fakeHash`; `nix build .#program` then says what the hash is. In
   `nix/site.nix`, the program's name.
4. Change what you like: `content/site.toml` first, then `web/` and
   `pages.go`. `nix run .#dev` serves it while you do.
5. Add the site to the host: `services.small-press.sites.<name>`, with its
   domain, its repository and the program's name, and its deploy's user,
   `small-press-<name>`, in `nix.settings.allowed-users` (the module says so
   when it isn't). For the receiver: a token, a deploy key with write access
   to that repository only, the forge's `knownHosts`, the `pushUrl` and the
   commits' `author`.

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

Nix doesn't see it: `nix build` takes the library at the release `go.mod`
pins.

## Moving a site to a new release of the library

1. Release the library: its changelog's Unreleased becomes the version, and
   the commit is pushed and tagged `v` and the version (`v0.2.0`).
2. In the site: `GOWORK=off go get codeberg.org/BW20/small-press@v0.2.0` and
   `GOWORK=off go mod tidy`.
3. Set `vendorHash` to `lib.fakeHash` first, then to what
   `nix build .#program` says, and run `./check.sh` and `nix flake check`.
   With the old hash left in place, Nix reuses the old vendor folder, which
   fails as inconsistent vendoring rather than giving the new hash.

A change to the library that a site's pages don't compile against shows
here, as a compile error. Each site moves when it is ready: the library
has no stable API before 1.0.0, and two sites may run two releases of it.

The host's module comes from the host's own pin of this flake. It runs each
site's program with flags, so a change to the receiver's flags needs the
module and every site moved together.
