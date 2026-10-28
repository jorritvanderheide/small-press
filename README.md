# small-press

**A site from a folder of Markdown, written in Obsidian and built to be
cited: a Go library, and a small program of each site's own on top of it.**

One Go program reads a folder of Markdown and writes a static site, which
nginx serves. Posts are written in Obsidian and published with the
[In the Open](https://codeberg.org/BW20/obsidian-in-the-open) plugin, over
Micropub, with their citations already formatted. small-press is that
program's library: each site is a program of its own, with its own pages,
templates and stylesheet, and the library does the rest. See
[Running it](#1-running-it).

<br/>

[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue?style=flat-square)](LICENSE)

A blog post that cites its sources should be as easy to cite as a paper.
Each post on a small-press site has its references, formatted once, in one
style, and a page for every work it cites, with every post that cites it.
It carries the tags Google Scholar and Zotero read, a BibTeX entry, and an
address that never changes.

The rest stays small. There's no JavaScript and no server program for
visitors: a blog only changes when something is published, so every page is
decided when the site is built. Images are dithered, and posts don't go
away, because a citation has to keep working.

<br/>

## 1 Running it

**On a server**, sites run on NixOS from this flake's module:
`services.small-press.sites`, nginx serving each site, which the host
builds itself from every push to the site's repository. See
[Deployment](docs/architecture.md#deployment).

**A new site** starts as a copy of `example/`. See [Sites](docs/sites.md).

**To work on it**, `nix develop` gives everything, and `nix run .#dev`
serves the example on <http://localhost:8080>, rebuilt on every request.
See [Running](docs/architecture.md#running).

<br/>

## 2 Publishing

Posts are written in Obsidian, and the In the Open plugin sends them to the
site's receiver, which commits them to the site's repository. A post is
published when the host has built that commit. A post can be sent as a
draft first, to see it on the site before anyone else does. A post can be
withdrawn, but never deleted: its address keeps working.

<br/>

## 3 Documentation

For whoever works on it:

- [**Architecture**](docs/architecture.md) - What the library is made of,
  where it ends and a site begins, the rules every site keeps to, and
  deployment.
- [**Sites**](docs/sites.md) - What a site's repository holds, how one
  starts, and how it moves to a new release of the library.
- [**Publishing**](docs/publishing.md) - The receiver: how a post from
  In the Open becomes a commit.

<br/>

## 4 License

The code is licensed under the [EUPL-1.2](LICENSE).

This is the library of a few people's sites, public to read and reuse
under that licence, but not a project yet: it has no stable API, and each
site pins the release it builds with, so a release may break a site that
moves to it. There's no support, and issues and pull requests aren't
taken.
