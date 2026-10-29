# Security policy

## Supported versions

Security fixes go into the latest release of small-press, and into `main`.
Older releases don't get separate fixes, so please move your site to the
latest release before you report.

## Reporting a problem

Please don't open a public issue for a security problem. Codeberg, where the
code is, has no private reports, so report it privately through the GitHub
mirror instead:

https://github.com/jorritvanderheide/small-press/security/advisories/new

Include the small-press release your site pins (in its `go.mod`), the NixOS
version (`nixos-version`), your `services.small-press.sites` configuration,
and the steps to reproduce it. Leave out the receiver's token and deploy
key. Reports are looked at before anything about them is made public.

## What counts

A site's pages need no JavaScript, and there is no server program for
visitors: nginx serves what the build wrote, with a content security
policy that runs only script files from the site's own `web/static/`,
never inline script or another site's. The one program that runs is the
receiver, which only nginx can
reach, which wants the site's token, and which only commits to the site's
repository; see [Publishing](docs/publishing.md). Each site's deploy and
receiver run as users of their own; see
[Deployment](docs/architecture.md#deployment).

So these are security problems:

- A post, a work, a page or an image that runs as script on the site, or
  that writes a link other than the ones in
  [Rules](docs/architecture.md#rules).
- A request to the receiver without the site's token that changes anything,
  or that reads anything but an upload by its address.
- A request with the token that writes outside `content/`, outside the
  receiver's state directory, or to a post without `generator`.
- Content in a site's repository that makes the build or the receiver run
  code, or read or write files outside the site's own.
- The receiver's token or deploy key ending up where others can read them:
  the Nix store, a log, a commit, the deploy's user.
- One site's build, deploy or receiver that can read or change another
  site's.
- The build's nginx includes, or anything else outside the build's site,
  served to readers.

These are not, on their own:

- Anything that needs root on the host, or the site's token, to begin with,
  except as above.
- A draft that someone finds: its address is unlisted, not secret, since the
  repository is public.
- Code in a site's own program, pages or templates.
- Problems in nginx, Go or goldmark themselves. Report those to them.

A bug that breaks a site or loses a post is serious too, but it isn't
secret: those don't need a private report.
