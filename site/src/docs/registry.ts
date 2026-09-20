/**
 * The documentation, read straight out of the repository.
 *
 * `docs/*.md` is loaded at build time rather than copied into the site, so
 * there is exactly one copy of every page. The site cannot drift from the
 * documentation that ships with the source, because it is the same file.
 */

const sources = import.meta.glob("../../../docs/*.md", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>

export type Doc = {
  /** URL segment, from the file name: docs/agent.md becomes "agent". */
  slug: string
  /** The page's own H1, or its slug if it somehow has none. */
  title: string
  /** The file's path in the repository, for the "edit this page" link. */
  path: string
  markdown: string
}

/** Every page, keyed by slug. */
export const DOCS: Record<string, Doc> = Object.fromEntries(
  Object.entries(sources).map(([file, markdown]) => {
    const slug = file.slice(file.lastIndexOf("/") + 1).replace(/\.md$/, "")
    return [
      slug,
      {
        slug,
        title: firstHeading(markdown) ?? slug,
        path: `docs/${slug}.md`,
        markdown,
      },
    ]
  }),
)

/**
 * The reading order, which is not alphabetical: it goes from "I have a bare
 * machine" to "I am changing Runnerly". A page missing from here is a
 * mistake, so the nav falls back to listing it rather than hiding it.
 */
const ORDER = [
  "getting-started",
  "installation",
  "github",
  "runners",
  "agent",
  "docker",
  "ephemeral-runners",
  "server",
  "dashboard",
  "operations",
  "configuration",
  "security",
  "troubleshooting",
  "architecture",
  "development",
] as const

/** One group in the sidebar. */
export type Group = { name: string; slugs: string[] }

const GROUPS: { name: string; slugs: string[] }[] = [
  { name: "Start here", slugs: ["getting-started", "installation", "github"] },
  { name: "Running runners", slugs: ["runners", "agent", "docker", "ephemeral-runners"] },
  { name: "A fleet", slugs: ["server", "dashboard", "operations"] },
  { name: "Reference", slugs: ["configuration", "security", "troubleshooting"] },
  { name: "Internals", slugs: ["architecture", "development"] },
]

/**
 * nav returns the sidebar. Anything in docs/ that no group claims is
 * appended under "More", so adding a page to the repository makes it appear
 * here without anyone remembering to edit this file.
 */
export function nav(): Group[] {
  const claimed = new Set(GROUPS.flatMap((g) => g.slugs))
  const groups = GROUPS.map((g) => ({
    name: g.name,
    slugs: g.slugs.filter((s) => s in DOCS),
  })).filter((g) => g.slugs.length > 0)

  const rest = Object.keys(DOCS)
    .filter((s) => !claimed.has(s))
    .sort()
  if (rest.length > 0) groups.push({ name: "More", slugs: rest })

  return groups
}

/** The flat reading order, for the previous and next links on a page. */
export function readingOrder(): string[] {
  return nav().flatMap((g) => g.slugs)
}

/** Where a reader starts. */
export const FIRST_DOC = ORDER[0]

function firstHeading(markdown: string): string | undefined {
  return /^#\s+(.+)$/m.exec(markdown)?.[1].trim()
}

/** slugify matches the anchors GitHub generates, so in-page links work. */
export function slugify(text: string): string {
  return text
    .trim()
    .toLowerCase()
    .replace(/[^\w\s-]/g, "")
    .replace(/\s+/g, "-")
}
