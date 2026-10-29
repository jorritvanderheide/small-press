# Architecture

small-press makes a site from a folder of Markdown: a blog, in English,
whose posts are written in Obsidian and arrive through the In the Open
plugin, over Micropub, with their citations already formatted; anything
else in `content/` is written by hand in the same format. It is a Go
library, and each site is a small Go program of its own on top of it, with
its own pages, templates and stylesheet. [Publishing](publishing.md)
describes the receiver that takes posts, and [Sites](sites.md) how a site's
program is made.

A site has no server program, and its pages need no JavaScript. A blog
only changes when something is published, so everything a request could
need is decided at build time. The receiver is the one program that runs,
and all it does is turn a published post into a commit.

## Where things live

| Path | What it holds |
| --- | --- |
| `smallpress.go` | `Main`, a site's program: flags, and the three ways to run: build once, serve while you write, or receive posts. `Site`, what a site gives it. |
| `receive.go` | The receiver's flags, and its wiring. |
| `content/` | Reading `content/`: front matter, Markdown, images. Nothing about HTML. |
| `render/` | goldmark, its extensions, and the passes over each post's syntax tree. |
| `site/` | The site as a whole: posts, threads, works, backlinks, and every check that needs more than one post. |
| `build/` | Every file but the pages: feed, sitemap, `cite.bib` files, images, robots.txt, nginx's includes, the static files; what every site's pages say alike (a post's text, references, citation and its tags); and `Verify`, that the pages are all there. |
| `dither/` | Ordered dithering of images, as MeetINN's. |
| `micropub/` | The receiver: Micropub requests in, commits out. |
| `example/` | A whole site on the library, for its tests and for a new site to start from: its program, pages, `web/` and `content/`. |
| `flake.nix` | The example's two packages, `program` and `site`, and the module. |
| `nix/` | The NixOS module and its VM test (with its stand-in site, `nix/test/`), and the example's `program.nix` and `site.nix`. |
| `docs/` | This, and the rest of the developer docs. |

`content` and `site` are pure: no filesystem except through an `fs.FS` they
are given, so every decision is tested on an in-memory site.

## The library and a site

**The library decides what is true about the content; a site decides how it
looks.** The line runs where it does so that every site keeps the rules
below, however different its pages are:

- **The library's:** the content's format, every check, the addresses
  (`/log/<slug>/`, `/threads/<name>/`, `/works/<key>/`, `/<page>/`), the
  receiver, and every file that isn't a page. Also what each post's page
  says the same way on every site, so a fix reaches them all: its text
  (`build.PostText`), its references (`build.References`), "Cite this post"
  (`build.Citation`, `build.BibTeX`), and the tags Google Scholar and Zotero
  read, with the post's JSON-LD (`build.Meta`).
- **The site's:** which pages it has and what each gets, its templates and
  static files, and the ids its post page takes for its own sections
  (`site.Options`), which a heading in a post can't have.

`smallpress.Site.Build` puts them together: the site's pages, the library's
files, and `build.Verify` between them, which fails the build when a page is
missing at an address the site gives something, or when the site writes a
file the library does. So a link the checks let through always leads
somewhere, whatever the site's pages are. The receiver checks a post with
the same `Build`, the site's pages included, so what it takes is what the
deploy will build.

The library has no stable API before 1.0.0: there are only a few sites, and
each pins the release it builds with. A release that breaks a site shows when
that site moves to it, as a compile error rather than at run time.

## Content

```
content/
  log/<slug>/index.md     a post, with its images next to it
  pages/<name>.md         about, colophon, library: pages outside the log
  works/<key>.toml        a work a post cites, formatted once
  threads.toml            optional: a thread's description, their order, or a page kept
  site.toml               the site's name, address, author and ORCID, line and menu
```

Nothing else belongs in `content/`: anything else there fails the build, so
a file in the wrong place is found rather than left out. A slug, a page's
name and a thread's name are lowercase letters, digits and hyphens.

A thread exists while a post names it: a post published with a new one makes
its page, and when no post names it any more, the page goes. Nothing about a
post depends on its threads, so no post's address changes. `threads.toml`
only describes a thread, puts the threads in order (the ones it doesn't list
come after, by name), or keeps a thread's page when no post is in it, such as
one the menu links to. A thread only a draft names has a page, for the draft's
own, but isn't in the sitemap.

The directory name is the post's slug, and **a slug never changes**: people
cite the URL. A post that must move lists its old slugs under `aliases`, and
the old URL redirects to the new one for good.

A work's key is in its URL too, so **a key never changes** either. Keys are
pinned in Better BibTeX, not regenerated when a work's metadata changes.

### Front matter

TOML between `+++` lines.

```toml
+++
title = 'The NixOS module system is a quiet revolution'
summary = 'Why declarative configuration changes how we think about infrastructure.'
date = 2026-03-15T10:00:00Z
updated = 2026-04-02T09:00:00Z     # optional, when the text changed in substance
threads = ['tech']
aliases = []                       # optional, earlier slugs
generator = 'micropub'             # set by the receiver, absent on hand-written posts
references = ['marsh2020']         # keys of the works cited, in reference-list order
doi = '10.64000/abcd-1234'          # optional, once the post has one
withdrawn = 2026-11-02T08:00:00Z   # optional, when the post was withdrawn
withdrawn-reason = 'The figures in section 2 are wrong.'   # optional, why
draft = true                       # optional, until the post is published
+++
```

- `title`, `summary` and `date` are required. A post without them fails the
  build, with the file and the field.
- `references` lists the key of every Zotero work the post cites, in the
  order of the reference list.
- `generator` marks what the receiver may replace or withdraw. It never
  touches a post without it.
- `doi` is the post's DOI, once it has one: from Rogue Scholar, which
  registers one for each post it archives. It is set by hand, and the
  receiver keeps it when it updates the post.
- `withdrawn-reason` is plain text, a sentence for readers, and only with
  `withdrawn`.
- `draft` makes the post a draft; see [A draft](#a-draft).

### Works

Each work is formatted once, in `content/works/<key>.toml`, and every post
that cites it shares that one text:

```toml
text = 'Marsh, Ann. 2020. *Archives and Their Readers*. Leiden: Brill.'
url = 'https://doi.org/10.1163/example'   # optional
```

The text is formatted by pandoc's citeproc in the plugin. The site never
formats a citation itself: one style, decided where the bibliography is. The
receiver rewrites a work's file each time it takes a post that cites it, so
correcting a work in Zotero and republishing any one post corrects it
everywhere.

### The body

CommonMark with GFM tables, strikethrough, footnotes and task lists, a
task's box a checkbox no reader can tick. Two kinds of link have a meaning
of their own:

- **A link to another post**, `/log/<slug>/`, is an ordinary link. The site
  reads every such link to build backlinks, so there is nothing to keep in
  step by hand. A link to the site's own address,
  `https://example.test/log/<slug>/` with the address in `site.toml`, is the
  same link: the page writes it as
  `/log/<slug>/`.
- **A citation** is a link to its entry in the post's reference list,
  `#ref-<key>`, with the in-text form as its words:
  `[(Marsh 2020, 12)](#ref-marsh2020)`. That is what pandoc writes, so a post
  from In the Open and one written by hand are written alike. Every key cited
  in the body is in `references`, every key in `references` is cited and has
  a file in `content/works/`, or the build fails.

Every other link is just a link. Further reading, a web page that isn't
cited, goes in the body as a list of links under its own heading:
`references` is only for works the post cites.

Images are Markdown images of files in the post's folder. An image in a
paragraph of its own, followed by a paragraph that is all italics, is a
figure, the italics its caption, as In the Open sends it; so is an image
with the italics on the line right under it, as Obsidian shows it. A
figure is written as a `<figure>` with a `<figcaption>`. An image without
such italics is only an image: its alt text is not a caption.

## Pages and files

Every site has a page or a file at each of these addresses, which
`build.Verify` checks: the library writes the files, and the site the pages.
What a page shows is the site's; here, the example's.

| URL | What it is |
| --- | --- |
| `/` | The newest posts, and what the site is. |
| `/log/` | Every post, newest first, grouped by year. |
| `/log/<slug>/` | A post: its text, references, "Linked from", and "Cite this post". |
| `/log/<slug>/cite.bib` | The post as a BibTeX entry. |
| `/threads/<thread>/` | The posts in a thread, under its description if `threads.toml` gives one. |
| `/works/<key>/` | A work you cited: its reference, and every post that cites it. |
| `/<name>/` | A page from `content/pages/`. |
| `/feed.xml` | Atom, every post in full. |
| `/sitemap.xml`, `/robots.txt`, `/404.html` | As the names say. |

### A post, in the example

From top to bottom:

1. The title, the exact date (*15 March 2026*, in `<time datetime>`), when
   it was updated, if it was, and its threads. A reader citing it needs the
   date as it is.
2. The text, with footnotes.
3. **References**: `references` in order, each with `id="ref-<key>"`, which
   the in-text citations link to, with the reference as their tooltip. Each
   links on to its work's page, with every post that cites it.
4. **Linked from**: every post whose body links to this one, newest first.
   Left out when there is none.
5. **Cite this post**: one formatted citation, in a fixed author-date form,
   and the BibTeX entry in a `<details>`, with a link to `cite.bib`. With
   a DOI, the citation and the entry give the DOI rather than the address.

Every heading in the text has an `id`, made from its words, so a section can
be linked to and cited: `/log/<slug>/#method`. One the page uses for its own
sections (`site.Options`; the example's are `references`, `linked-from` and
`cite`) fails the build.

### A draft

A draft is a post only its address leads to, for seeing it as readers will
before they can. Its page is the post's, with a line saying it is a draft,
and `noindex` for search engines. It is in none of the lists: not `/log/`,
its threads, the feed, the sitemap, nor any "Linked from" or works page. A
published or withdrawn post, or a page, that links to it fails the build,
since the link would lead readers there; a draft may link to another. Its address is unlisted,
not secret: the repository is public.

The receiver publishes it when In the Open says so, and its `date` becomes
that moment. A draft is never withdrawn: it was never public, so it is
discarded instead, its folder deleted. A draft with `withdrawn` fails the
build.

### A withdrawn post

A post is withdrawn, never deleted: other posts link to it, and people may
have cited it. As a journal keeps a retracted article, a withdrawn post
keeps its page, text and all, and its `cite.bib`. Above the text, the page
says it was withdrawn, when, and why, from `withdrawn-reason`, and it has
`noindex` for search engines. It is served as any page is, with `200`: a
DOI's landing page has to keep answering. It leaves `/log/`, its threads,
the feed, the sitemap, and every "Linked from" and works page. Links to it
still build, and lead to its page.

Taking a post down for good, for legal or privacy reasons, is done by hand:
delete its folder, and the build names every post that still links to it.

### Being cited

Making a post easy to cite is a feature of every page, not a page of its own.

- **Citation tags in the head:** `citation_title`, `citation_author`,
  `citation_publication_date`, `citation_public_url` and the rest of the
  Highwire set, which Google Scholar reads and Zotero's connector saves with
  one click, with `site.toml`'s author. Also schema.org
  `BlogPosting` as JSON-LD (all of it `build.Meta`), and Open Graph.
  With a DOI, `citation_doi` too.
- **The author's ORCID iD**, `orcid` in `site.toml`, in the JSON-LD, the
  feed's `<author>`, and a link at the foot of every page. It is checked when
  the site is read, check digit and all, so a typo fails the build rather
  than point at someone else.
- **`cite.bib`** next to each post, and the same entry on the page.
- **Stable URLs**, by the rule above, and a canonical link on every page.
- **The feed has every post in full**, with a stable `<id>` per post, its
  threads as `<category>` and the licence as `<rights>`, which is what
  [Rogue Scholar](https://rogue-scholar.org) needs to archive a blog and
  give each post a DOI.
- **Posts are under CC BY 4.0**, as Rogue Scholar asks, which the feed and
  the JSON-LD say, and every page of the example says with `rel="license"`.
  The code is EUPL-1.2.
- **A page prints as it should** (`@media print`): the text, its references
  and how to cite it, without the menu, and with each web link's address
  after it, so "Save as PDF" gives a copy fit to file.

## Time

Nothing on the site depends on when it is built: a Nix build is pure, and
the same commit gives the same site.

- **Dates as seasons, in the lists.** The log, the home page and the
  threads give a post's date as a season, *early spring 2026* (`season`, in
  `build.Funcs`). A post's own page gives it exactly.
- **Posts stay.** They don't fade or expire: a post may be cited, and a
  citation has to keep working.
- **The build number** at the foot of every page is the commit count
  (`revCount`), so there is no counter file to commit.

## Rules

- **No JavaScript needed.** Every page works without it: folding is
  `<details>`, dark mode is `prefers-color-scheme`. A site may add a script
  of its own, a file in its `web/static/`, for what only adds to a page,
  such as a button that copies a citation that can be selected by hand
  too. nginx's policy (`default-src 'none'; script-src 'self'`) runs those
  files and nothing else: no inline script, and nothing from another site.
  A post can't add a script, as raw HTML stays off and an upload is only
  ever a PNG or a JPEG. The one `<script>` in a page is a post's JSON-LD,
  which is data: no browser runs it.
- **A broken site does not build.** A link to a post that does not exist, a
  citation without a reference, a reference never cited, a reference with no
  work in `content/works/`, a thread whose name isn't a name, a link to a
  thread no post names and `threads.toml` doesn't keep, a duplicate slug or
  alias, a page missing where the site gives something (`build.Verify`):
  each fails the build with the file it is in.
- **Rendered into memory, written at the end.** A template error never
  leaves half a site in the output.
- **Links are safe.** Only `http`, `https`, `mailto`, site-relative links
  and links to a place on the same page are written; raw HTML in Markdown
  stays off.
- **Images are dithered at build time** into 6-grey paletted PNGs no wider
  than 800 pixels, with ordered Bayer dithering. The originals stay in
  `content/`; nothing derived is committed.
- **Static files carry their hash** (`/static/site.css?v=1a2b3c4d`, from
  `build.Static`) and are cached
  for a year; everything else is revalidated (`Cache-Control: no-cache`),
  which its ETag answers.
- **The example's CSS is written by hand**, with no build step: custom
  properties, mobile
  first, `:focus-visible`, `prefers-reduced-motion`, and print.
- **The example is accessible by default:** a skip link, `aria-current` in
  the menu, a
  heading order that holds, alt text required on every image.

## Running

```sh
nix run .#dev        # the example on localhost:8080, rebuilt on every request, templates from disk
nix build            # the example site, in ./result
nix flake check      # the checks, the example's build, and the VM test
./check.sh           # gofmt, go vet, go test, staticcheck, govulncheck
```

`check.sh` and `nix flake check` are what CI runs, on the Forgejo runner on
dapple, which has KVM for the VM test: a local run and CI cannot disagree.

The flake builds the example as a site's flake builds the site
([Sites](sites.md)): `program` from everything but `example/content/`, and
`site`, that program run over it. The library's tests, and the example's,
run in the program's build. A Nix build is pure, so it dithers every image
again each time.

## Deployment

`nix/module.nix` is `services.small-press`, which the flake exports as
`nixosModules.default` for the host's own configuration to import. Each site
in `services.small-press.sites` has a name, and everything of its own is
named for it: for `bw20`, the users `small-press-bw20` and
`small-press-bw20-receiver`, the units `small-press-bw20-deploy` and the
rest, and `/var/lib/small-press-bw20`. Two sites share nothing but nginx, so
one site's build can't read the other's key. The module needs nothing
built: it serves, and runs, what each site's deploy builds. The deploy runs
`nix build` as the site's user, so a host that limits
`nix.settings.allowed-users` adds it there; the module refuses to build
until it has.

**The deploy** builds the site on the host. A timer asks the site's
repository for the newest commit on `main` with `git ls-remote`, five
minutes after it last looked, and when that commit is new, builds the
flake's `site` and then its `program` from it, as links in the site's
folder in `/var/lib`. Each link moves in one step when its build is done;
the site first, which the program is part of, so a commit that doesn't
build changes nothing. The module runs `bin/<program>` from `program`, by
the site's `program` option.

- **Every push publishes.** A post from the receiver is built at once: the
  receiver writes the time to `pushed` in its state directory after each
  push, and a path unit starts the deploy when that changes. A push from
  elsewhere, such as a laptop, is live within five minutes and a build.
- **A broken commit never goes live.** The site already there stays, and the
  units in `deploy.onFailure` are started, once: the deploy doesn't try that
  commit again. To try it anyway, remove `failed` from the site's folder.
  One that publishes starts those in `deploy.onSuccess`, such as a report
  to a status page.
- **A forge that doesn't answer** is asked again at the next look, without a
  failure.
- **The host's configuration only changes with the module.** Posts,
  templates and the Go code, the library's included, all come with the
  site's build, which pins the library in its `go.mod`; only a change to
  nginx or systemd needs a rebuild of the host.
- **The fetch has the whole history**, so `revCount` is always there for
  the build number ([Time](#time)).
- **After a deploy**, nginx reads the build's includes again, and the
  receiver's socket starts and the receiver restarts with the program just
  built, so it checks posts by the same rules as the build that will
  publish them. Before a site's first deploy there is no program, so its
  socket and receiver wait for one (`ConditionPathExists`) rather than fail
  the host's switch.

**nginx** serves each site's build at its `domain`, with BW20's old site's
rules:

- Security headers, with a content security policy that runs only the
  site's own script files, never inline script or another site's.
- AI crawlers kept out twice: in `robots.txt`, and with a 403 for the same
  user agents, read from one list (`build/crawlers.txt`).
- Aliases as permanent redirects.
- No access log.
- TLS is the host's.

What depends on the content, the redirects and the crawlers' 403, the build
writes as nginx includes, in `nginx/server/` and `nginx/location/` of the
site. The module includes them by glob, so nginx starts before the first
deploy, when there is no site yet.

A site's `receiver` adds its receiver, as [Publishing](publishing.md)
describes.

`nix/test.nix` starts it in two VMs, without the network.

- **deploy** deploys two sites, each from a repository on the VM: nginx up
  and no failed receiver before the first deploy, a first commit, the
  receiver's link and socket once there is a program, the build's redirect
  and 403, a deploy with nothing new that does nothing, a commit that
  doesn't build and fails once, one that builds again, and a second site
  beside the first, as its own user. Building a real flake there would need nixpkgs and Go
  without the network, so the sites in it are a stand-in (`nix/test/`) that
  writes what the real build does.
- **site** serves the example site, built outside the VM and linked where a
  deploy would put it, as a reader gets
  it: its pages, headers and caching, the crawlers' 403 and redirects, the
  404 page, and the build's includes kept from readers.

The site VM runs the example's real receiver too, against a repository on
the VM with the example's content: it wants its token, refuses a post that
would break the build, or that the site's pages can't take, with the
build's own message, and commits and pushes one that wouldn't, which starts
the deploy at once.

## What comes from where

- **From BW20's old site:** no JavaScript needed, dates as seasons,
  dithered images, the build number, bot blocking, the hardened nginx
  module and its VM test, threads.
- **From MeetINN:** `html/template` with `embed.FS` and templates from disk
  in development, hashed asset URLs, rendering into memory first, goldmark
  with passes over the syntax tree, safe links, the dithering, `check.sh` for
  CI and local runs alike, and docs that write the rules down.
- **New:** citations and references from the plugin, publishing over
  Micropub, works pages, backlinks, citation tags, `cite.bib`, a full-text
  feed, withdrawn posts, slugs that never change, and sites of their own on
  one library.
