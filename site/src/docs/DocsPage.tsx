import { useEffect, useMemo, useState } from "react"
import { Link, NavLink, useParams } from "react-router-dom"
import { ArrowLeft, ArrowRight, Menu, PencilLine, Terminal as TerminalIcon, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { GithubMark } from "@/components/github-mark"
import { DOCS, FIRST_DOC, nav, readingOrder, slugify } from "@/docs/registry"
import { Markdown } from "@/docs/markdown"
import { cn } from "@/lib/utils"

const REPO = "https://github.com/bablilayoub/runnerly"

export function DocsPage() {
  const { slug = FIRST_DOC } = useParams()
  const doc = DOCS[slug]
  const [menuOpen, setMenuOpen] = useState(false)

  // Scrolling is the one thing here that is a real external system, so it
  // is the one thing that belongs in an effect. Closing the menu is not:
  // that is a consequence of the tap, and it is handled where the tap is.
  useEffect(() => {
    const hash = window.location.hash.slice(1)
    if (!hash) {
      window.scrollTo(0, 0)
      return
    }
    document.getElementById(hash)?.scrollIntoView()
  }, [slug])

  const order = useMemo(() => readingOrder(), [])
  const groups = useMemo(() => nav(), [])

  if (!doc) return <NotFound slug={slug} />

  const at = order.indexOf(slug)
  const prev = at > 0 ? DOCS[order[at - 1]] : undefined
  const next = at >= 0 && at < order.length - 1 ? DOCS[order[at + 1]] : undefined

  return (
    <div className="min-h-dvh bg-background text-foreground antialiased">
      <DocsNav onMenu={() => setMenuOpen((o) => !o)} menuOpen={menuOpen} />

      <div className="mx-auto flex w-full max-w-7xl gap-10 px-6">
        <Sidebar groups={groups} open={menuOpen} onNavigate={() => setMenuOpen(false)} />

        <main className="min-w-0 flex-1 py-10 lg:py-14">
          <article className="min-w-0 max-w-3xl">
            <p className="font-mono text-xs uppercase tracking-[0.18em] text-muted-foreground">
              Documentation
            </p>
            <h1 className="mt-3 text-balance text-3xl font-medium tracking-tight sm:text-4xl">
              {doc.title}
            </h1>

            <a
              href={`${REPO}/blob/main/${doc.path}`}
              target="_blank"
              rel="noreferrer"
              className="mt-4 inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <PencilLine className="size-3.5" />
              Edit this page
            </a>

            <hr className="my-8 border-border" />

            <Markdown markdown={doc.markdown} />

            <nav className="mt-14 grid gap-3 border-t border-border pt-8 sm:grid-cols-2">
              {prev ? <Neighbour doc={prev} direction="prev" /> : <span />}
              {next ? <Neighbour doc={next} direction="next" /> : null}
            </nav>
          </article>
        </main>

        <OnThisPage markdown={doc.markdown} />
      </div>
    </div>
  )
}

function DocsNav({ onMenu, menuOpen }: { onMenu: () => void; menuOpen: boolean }) {
  return (
    <header className="sticky top-0 z-40 border-b border-border/50 bg-background/80 backdrop-blur-xl">
      <div className="mx-auto flex h-14 w-full max-w-7xl items-center justify-between gap-3 px-6">
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onMenu}
            aria-label={menuOpen ? "Close the menu" : "Open the menu"}
            aria-expanded={menuOpen}
            className="-ml-2 grid size-9 place-items-center rounded-md text-muted-foreground transition-colors hover:text-foreground lg:hidden"
          >
            {menuOpen ? <X className="size-4" /> : <Menu className="size-4" />}
          </button>

          <Link to="/" className="flex items-center gap-2.5">
            <span className="grid size-6 place-items-center rounded border border-border">
              <TerminalIcon className="size-3.5" />
            </span>
            <span className="font-medium tracking-tight">Runnerly</span>
          </Link>
          <span className="hidden text-sm text-muted-foreground sm:inline">docs</span>
        </div>

        <nav className="flex items-center gap-1">
          <Button variant="ghost" size="sm" asChild className="rounded-full">
            <Link to="/">Home</Link>
          </Button>
          <Button size="sm" asChild className="rounded-full">
            <a href={REPO} target="_blank" rel="noreferrer">
              <GithubMark className="size-4" />
              GitHub
            </a>
          </Button>
        </nav>
      </div>
    </header>
  )
}

function Sidebar({
  groups,
  open,
  onNavigate,
}: {
  groups: { name: string; slugs: string[] }[]
  open: boolean
  onNavigate: () => void
}) {
  return (
    <aside
      className={cn(
        "shrink-0 lg:block lg:w-60",
        // On a phone the sidebar replaces the page rather than sitting
        // beside it; there is no room for both.
        open
          ? "fixed inset-x-0 bottom-0 top-14 z-30 overflow-y-auto border-t border-border bg-background px-6 py-6 lg:static lg:inset-auto lg:border-t-0 lg:px-0"
          : "hidden",
      )}
    >
      <div className="lg:sticky lg:top-14 lg:max-h-[calc(100dvh-3.5rem)] lg:overflow-y-auto lg:py-10">
        {groups.map((group) => (
          <div key={group.name} className="mb-7">
            <p className="mb-2 font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground/70">
              {group.name}
            </p>
            <ul className="space-y-0.5">
              {group.slugs.map((slug) => (
                <li key={slug}>
                  <NavLink
                    to={`/docs/${slug}`}
                    onClick={onNavigate}
                    className={({ isActive }) =>
                      cn(
                        "-ml-2.5 block rounded-md px-2.5 py-1.5 text-sm transition-colors",
                        isActive
                          ? "bg-card text-foreground"
                          : "text-muted-foreground hover:text-foreground",
                      )
                    }
                  >
                    {DOCS[slug].title}
                  </NavLink>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </aside>
  )
}

/** The table of contents, built from the page's own H2s. */
function OnThisPage({ markdown }: { markdown: string }) {
  const headings = useMemo(
    () =>
      [...markdown.matchAll(/^##\s+(.+)$/gm)]
        .map((m) => m[1].trim())
        .map((text) => ({ text, id: slugify(text) })),
    [markdown],
  )

  if (headings.length < 2) return null

  return (
    <aside className="hidden w-56 shrink-0 xl:block">
      <div className="sticky top-14 max-h-[calc(100dvh-3.5rem)] overflow-y-auto py-14">
        <p className="mb-3 font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground/70">
          On this page
        </p>
        <ul className="space-y-1.5 border-l border-border">
          {headings.map(({ text, id }) => (
            <li key={id}>
              <a
                href={`#${id}`}
                className="-ml-px block border-l border-transparent pl-3 text-sm text-muted-foreground transition-colors hover:border-foreground/40 hover:text-foreground"
              >
                {text}
              </a>
            </li>
          ))}
        </ul>
      </div>
    </aside>
  )
}

function Neighbour({
  doc,
  direction,
}: {
  doc: { slug: string; title: string }
  direction: "prev" | "next"
}) {
  const isNext = direction === "next"
  return (
    <Link
      to={`/docs/${doc.slug}`}
      className={cn(
        "group flex flex-col gap-1 rounded-xl border border-border p-4 transition-colors hover:bg-card/60",
        isNext && "sm:items-end sm:text-right",
      )}
    >
      <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
        {isNext ? null : <ArrowLeft className="size-3" />}
        {isNext ? "Next" : "Previous"}
        {isNext ? <ArrowRight className="size-3" /> : null}
      </span>
      <span className="font-medium tracking-tight">{doc.title}</span>
    </Link>
  )
}

function NotFound({ slug }: { slug: string }) {
  return (
    <div className="grid min-h-dvh place-items-center bg-background px-6 text-center">
      <div>
        <p className="font-mono text-sm text-muted-foreground">404</p>
        <h1 className="mt-3 text-2xl font-medium tracking-tight">
          There is no page called <code className="font-mono">{slug}</code>.
        </h1>
        <div className="mt-7 flex justify-center gap-3">
          <Button asChild className="rounded-full">
            <Link to={`/docs/${FIRST_DOC}`}>Getting started</Link>
          </Button>
          <Button variant="secondary" asChild className="rounded-full">
            <Link to="/">Home</Link>
          </Button>
        </div>
      </div>
    </div>
  )
}
