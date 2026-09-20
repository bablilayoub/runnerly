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

Two things are written here rather than installed:

- `src/components/terminal.tsx` — the terminal frame. No component library
  has one, and the page is mostly terminal output.
- `src/components/github-mark.tsx` — Lucide dropped brand icons, and pulling
  in a second icon package for one path would cost more than the path.

## Monochrome

Every color is `oklch(L 0 0)`: luminance with zero chroma. The shadcn Nova
preset is almost entirely that already; `--destructive` was the one hue and
it is neutralized in `src/index.css`, along with the dark palette this page
uses. There is no light theme — one page does not need a second design to
keep working.

If you add a color, it has no hue. That is the whole rule.

## install.sh

`npm run build` copies the repository's `install.sh` into `dist/`, because
the page tells people to run:

```bash
curl -fsSL https://runnerly.dev/install.sh | sh
```

There is one installer in the repository and this publishes that one, so the
page cannot end up advertising a stale copy. CI fails if it is missing.

## Deploying

`site/dist` is static files. Nothing here assumes a host. Whatever serves it
needs to serve `install.sh` as plain text — not as a download — so
`curl | sh` works.
