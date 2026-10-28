# Publishing

Posts come from Obsidian, through the
[In the Open](https://codeberg.org/BW20/obsidian-in-the-open) plugin. What it
sends is specified in its
[`docs/micropub.md`](https://codeberg.org/BW20/obsidian-in-the-open/src/branch/main/docs/micropub.md):
Micropub, and the references extension. This page is how a site on
small-press takes it.

The receiver is the third way to run a site's program, in `micropub/`.
It takes requests by that spec and turns each into a commit to the site's
repository. It never builds or serves the site; the repository stays the
only input, and the build stays reproducible. The host's deploy timer picks
the commit up like any other, as [Deployment](architecture.md#deployment)
describes.

For each request, one at a time:

1. Fetch the repository and reset to the remote's `main`.
2. Translate the request into files under `content/`, as the table below
   says.
3. Run the site's checks on `content/` with the change laid over it, in
   memory: everything its build does but write, its own pages included. A
   change that would break the build is refused with `400` and the build's
   own messages, and nothing is written.
4. Commit, as `publish: <slug>`, `draft: <slug>`, `update: <slug>`,
   `withdraw: <slug>`, `restore: <slug>` or `discard: <slug>`, and push. A
   rejected push is fetched and tried once more.
5. Write the time to `pushed` in its state directory, which starts the
   deploy at once ([Deployment](architecture.md#deployment)).
6. Answer: `202 Accepted` for a new post, `204 No Content` for the rest.

| Micropub | This site |
| --- | --- |
| `name` | `title` |
| `summary` | `summary`, required as for every post |
| `content` | the body of `content/log/<slug>/index.md` |
| `category` | `threads`; a new one becomes a thread of the site, and `q=category` lists every thread |
| `mp-slug` | the slug, else one made from the title |
| `published` | `date` |
| `updated` | `updated` |
| `post-status` | `draft = true` for `draft`; for `published`, a draft's `draft` removed, and its `date` set to now |
| `references` | the keys in `references`, and each work's `text` and `url` in `content/works/<key>.toml`; `csl` is not kept |
| delete, undelete | `withdrawn` set or removed, with `withdrawn-reason` from the delete's `reason`; a draft's delete removes its folder |

- **The text is kept as it comes.** Citations, `#ref-<key>`, and links to
  the site's own posts are what the site reads already (the architecture's
  [body](architecture.md#the-body)). Only an image the media endpoint holds
  moves, into the post's folder, and is linked by its file name.
- **A slug already taken**, by a post or an alias, is refused rather than
  numbered: slugs never change, so you choose.
- **A post without `generator`** is hand-written, and an update or withdrawal
  of it is `403`. Every post the receiver writes has
  `generator = 'micropub'`. An update keeps what the receiver doesn't
  take from a request: the date, the slug, the aliases and a `doi`.
- **A post edited elsewhere** since the receiver last wrote it, by hand or by
  another tool, isn't replaced without asking: an update is `409 Conflict`,
  naming the commit, unless it says `"mp-overwrite": true`, which In the Open
  sends once you agree. Who last changed it is `git log`'s author of its
  `index.md`: anyone but the receiver's `author`. Withdrawing or restoring it
  keeps its text, so isn't refused.
- **Requests are JSON**, as In the Open sends them, and an update replaces
  what changed (`replace`); `add` and `delete` aren't taken. `q=source`
  answers with the post as it is in the repository, or only the properties
  asked for: its text with images by their file names, its references from
  their works' files, and its `post-status`. In the Open compares it with the
  note before an update.
  A slug, an alias and a published post's date never change through the
  receiver.
- **Uploads** are PNG or JPEG, at most 20 MB. They wait in the receiver's
  state directory, named by their hash and served at their URL, and are
  removed after a day if no post takes them. Next to its post, an image is
  named by the start of its hash.

A site's `receiver`, in `services.small-press.sites`, runs it: `enable`,
`tokenFile`, `pushUrl`, `sshKeyFile` (a deploy key for this repository
only), the forge's `knownHosts`, and the commit `author`. The token and
the key reach it as systemd credentials, so they stay out of the Nix store.

- **It runs the program the deploy built**, not one from the host's
  configuration, and restarts after each deploy. So its checks are the ones
  the build that publishes the post will run. Before a site's first deploy
  there is no program, so its socket and receiver wait for one: a request
  gets nginx's `502`, and no unit fails, which would fail the host's
  switch.
- **It runs as its own user**, not the deploy's, so the commits the host
  builds can't read its key.
- **nginx passes `/micropub` to it**, over a socket only nginx may open, and
  the home page gets the `Link: </micropub>; rel="micropub"` header.
