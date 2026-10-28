# Changelog

What changed in the library, newest first. There are no releases: each
site pins the commit it builds with, so changes collect under Unreleased,
and the commit is the version.

## Unreleased

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
