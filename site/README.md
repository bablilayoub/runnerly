# runnerly.dev

The landing page. It is not part of any binary: `runnerly-server` embeds the
dashboard from `web/`, and this is a separate static site.

```bash
make site-dev     # hot reload on :5173
make site         # build into site/dist
make site-check   # typecheck and lint
```

## What is in here

React, Vite, Tailwind v4 and [shadcn/ui](https://ui.shadcn.com) components,
which is why this directory has dependencies the rest of the project would
refuse. The dashboard keeps to three, because it ships inside a binary people
run on their infrastructure. A marketing page has no such constraint, and
there is no reason to hand-write an accordion.

Three things are written here rather than installed:

- `src/components/session.tsx` — replays a terminal session, typing the
  commands out. Runnerly is a command line tool, so the most honest thing the
  page can show is the thing running; every line is copied from real output
  and the replay only controls when each one appears.
- `src/components/terminal.tsx` — the static terminal frame, for output that
  should be readable at a glance rather than played.
- `src/components/github-mark.tsx` — Lucide dropped brand icons, and pulling
  in a second icon package for one path would cost more than the path.

## Motion

`src/components/motion.tsx` wraps [motion](https://motion.dev). Two rules:

**Reduced motion means no movement.** Content still appears; it just arrives
instead of sliding. The session replay prints in full immediately.

**An animation can only ever add to the page.** `Reveal` starts at zero
opacity, so a reveal that never fires would leave a section invisible with
its text sitting in the DOM — worse than no animation, and silent. It
therefore shows its content after 2.5 seconds regardless of what the
intersection observer did. This is not hypothetical: it was caught by
checking computed opacity after scrolling to the bottom, with two blocks
stuck at zero.

Worth knowing when testing: an embedded or backgrounded browser throttles
timers hard, so the replay will look stalled. Measure before concluding
anything is slow — openhole.dev shows the same throttling in the same pane.

## The logo

The artwork lives once, in `assets/` at the repository root, and both the
site and the GitHub README read it from there. The site imports it through
Vite, which hashes and bundles it; the README references the same files by
path. One copy, so the two can never show different marks.

| File | Where it is used |
| --- | --- |
| `runnerly-logo.png` | the full lockup, white — the site's header, its footer, the giant one across the bottom of the footer, and the README on a dark theme |
| `runnerly-logo-dark.png` | the same lockup in near-black, for the README on a light theme |
| `runnerly-mark.png` | the mark alone, which the favicon is built from |
| `runnerly-icon.png` | the mark on a dark plate: the dashboard's favicon, and what `site/public/favicon.png` is a copy of so the site can serve it from `/` |

The README uses `<picture>` with `prefers-color-scheme`, because the logo is
white and GitHub has a light theme: without it, half of all readers would
see nothing at all.

## Components from elsewhere

`src/components/ui/` is vendored — shadcn and ui-layouts copy source in
rather than linking a package, which means these files are ours to fix.

`spotlight.tsx` came from [ui-layouts](https://ui-layouts.com/components/spotlight-cards)
and needed three changes before it could ship:

- It attached a `window` mousemove listener **per card** and called
  `setState` from each. Six cards meant six React renders on every mouse
  move. The pointer is now tracked once per group, coalesced into an
  animation frame, and written to CSS custom properties through a ref, so
  moving the mouse re-renders nothing.
- Its `SpotlightCard` variant was hardcoded to slate and indigo. This page
  has no hue, so that variant is gone.
- It opened with `@ts-nocheck`.

The idea it contributed is the good part and is kept: one gradient in
viewport space behind every card's border at once, so the grid lights up
coherently around the pointer instead of each card reacting alone.

## Monochrome

Three typefaces, each with a job: **Bricolage Grotesque** for display
(headlines, section titles, the footer wordmark), **Geist** for reading, and
**Geist Mono** for every line of terminal output, which is most of the page.

Every color is `oklch(L 0 0)`: luminance with zero chroma. The shadcn Nova
preset is almost entirely that already; `--destructive` was the one hue and
it is neutralized in `src/index.css`, along with the dark palette this page
uses. There is no light theme — one page does not need a second design to
keep working.

If you add a color, it has no hue. That is the whole rule.

## The docs

`/docs/:slug` renders `docs/*.md` from the repository. They are read at build
time with `import.meta.glob`, not copied, so there is one copy of every page
and the site cannot drift from the documentation that ships with the source.
Editing `docs/agent.md` changes the page.

- The sidebar grouping is in `src/docs/registry.ts`. A page no group claims
  still appears, under "More", so adding a file to `docs/` cannot make it
  invisible.
- Cross-references between pages (`[the agent](agent.md)`) are rewritten to
  routes, so following one does not send the reader to GitHub. Links out of
  `docs/` still go to the repository.
- Headings get the same anchors GitHub generates, so `#the-core-risk` works
  in both places.
- Code blocks are deliberately not syntax highlighted. The palette has no
  hue, and most blocks are terminal output where color would imply meaning
  the output does not carry.

`npm run check-docs` fails on a link to a page that does not exist, and on a
page the sidebar does not list. It runs before every build and in CI, because
a broken cross-reference is now a 404 rather than a dead link on GitHub.

## install.sh

`npm run build` copies the repository's `install.sh` into `dist/`, because
the page tells people to run:

```bash
curl -fsSL https://runnerly.dev/install.sh | sh
```

There is one installer in the repository and this publishes that one, so the
page cannot end up advertising a stale copy. CI fails if it is missing.

## Deploying

`site/dist` is static files. Two things the host has to get right, and both
of them are invisible in development:

**The SPA fallback.** `/docs/agent` is a client-side route, not a file.
Without a rewrite to `index.html` the landing page works and every link off
it 404s — which nobody notices until someone else follows one.

**install.sh as text.** It is piped into a shell, so it has to arrive as
`text/plain` rather than as a download, and it must never fall through to
the SPA handler, which would hand the shell an HTML page.

### On a container host

The `Dockerfile` at the repository root builds the site and serves it with
Caddy, using `deploy/site/Caddyfile` for both of those rules. For Dokploy,
Nixploy, or any plain Docker host.

```bash
docker build -t runnerly-site .
docker run --rm -p 8080:80 runnerly-site
```

**It is at the root deliberately.** The site is built from four things —
`site/`, `docs/`, `assets/` and `install.sh` — so the build context has to
be the repository root. Most hosts take the context from wherever the
Dockerfile sits, so keeping it in a subdirectory silently made the context
that subdirectory: every `COPY` failed with "not found", and nothing in the
error mentioned the context. At the root, the default is correct and
nothing needs configuring.

If your host has a separate context setting, it should be `.` — the
repository root.

The build also fails if `install.sh` did not reach `dist/`, so a broken copy
step cannot ship a page whose one advertised command 404s.

### On Netlify or Cloudflare Pages

`public/_redirects` and `public/_headers` cover the same two rules there.
They are read by those hosts and **ignored by everything else** — on a plain
nginx or Caddy server they are inert files, which is what `deploy/site/`
exists for.

### Verifying a deployment

These are the checks worth running against a real deployment, because each
one covers a failure that only appears there:

```bash
curl -o /dev/null -w '%{http_code}\n'  https://runnerly.dev/docs/agent   # 200, not 404
curl -o /dev/null -w '%{content_type}\n' https://runnerly.dev/install.sh # text/plain
curl -fsSL https://runnerly.dev/install.sh | sh -n                        # valid shell
```

`npx vite preview` has its own fallback built in, so it will happily serve
deep links whether or not any of this is configured. It proves the build,
not the hosting.
