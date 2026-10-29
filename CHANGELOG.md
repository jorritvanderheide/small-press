# Changelog

What changed in the library, newest first. Each release is a tag, `v` and the
version, which a site pins in its `go.mod`; changes collect under Unreleased
until the next one. Before 1.0.0 there is no stable API: a release may break
a site's pages, and says so here.

## 0.1.3

- A site may have scripts of its own, files in its `web/static/`, for what
  only adds to a page: nginx's policy now has `script-src 'self'`. Pages
  still need no JavaScript, and no inline script or another site's runs.
  It reaches a host when its small-press input is updated.

## 0.1.2

- A reference's plain text keeps an address written as an autolink, as a
  style writes a DOI: the tooltip on a citation, the `citation_reference`
  tags and a work page's title lost it, leaving ". .".
- An image with a caption, the italics in the paragraph after it or on
  the line under it, is a `<figure>` with a `<figcaption>`. A site styles
  `figure` and `figcaption`; the example does.

## 0.1.1

- `render.Heading` has the heading's `Level`, 2 for `##`, so a site can
  build a table of contents from `Doc.Headings`. A `Heading` written
  without field names no longer compiles.

## 0.1.0

- The BW20 blog's program, as a library: each site is a program of its
  own, with its own pages, templates and stylesheet, on top of it. Every
  file but the pages, every check and the receiver are the library's.
- What every post's page says alike comes from the library: its text, its
  references, "Cite this post", and the tags Google Scholar and Zotero
  read, with the post's JSON-LD.
- A build fails when a site's pages leave out a page the site has an
  address for, so a link the checks let through always leads somewhere.
- Every file in a site's `web/static/` is published, each at an address
  with its hash.
- The NixOS module serves any number of sites, each from its own
  repository, with its own users, deploy and receiver:
  `services.small-press.sites.<name>`.
- `example/`, a whole site to start a new one from.
- A new site's receiver waits for its first deploy, instead of failing the
  host's switch when a request comes before there is a program to run.
