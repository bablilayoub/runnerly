// Every cross-reference between the docs is now a route on this site, so a
// link to a page that does not exist is a 404 rather than a dead link on
// GitHub. That is worse and quieter, so it is checked here.
//
// It also catches the reverse: a docs page nobody links to and the sidebar
// does not list, which is a page no reader will ever find.
import { readFileSync, readdirSync } from "node:fs"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const docsDir = resolve(here, "..", "..", "docs")

const files = readdirSync(docsDir).filter((f) => f.endsWith(".md"))
const slugs = new Set(files.map((f) => f.replace(/\.md$/, "")))

const problems = []

for (const file of files) {
  const text = readFileSync(join(docsDir, file), "utf8")
  for (const [, , href] of text.matchAll(/\[([^\]]*)\]\(([^)]+)\)/g)) {
    if (/^(https?:|mailto:|#)/.test(href)) continue

    const [path, anchor] = href.split("#")
    if (!path) continue

    // A link out of docs/ (../README.md, ../deploy/...) is rendered as a
    // link to the repository, which is correct and not checked here.
    if (path.startsWith("../")) continue

    if (!path.endsWith(".md")) {
      problems.push(`${file}: ${href} is not a docs page`)
      continue
    }
    const slug = path.replace(/\.md$/, "")
    if (!slugs.has(slug)) {
      problems.push(`${file}: links to ${path}, which does not exist`)
      continue
    }
    if (anchor) {
      const target = readFileSync(join(docsDir, `${slug}.md`), "utf8")
      const anchors = new Set(
        [...target.matchAll(/^#+\s+(.+)$/gm)].map(([, h]) =>
          h.trim().toLowerCase().replace(/[^\w\s-]/g, "").replace(/\s+/g, "-"),
        ),
      )
      if (!anchors.has(anchor)) {
        problems.push(`${file}: links to ${href}, and ${slug} has no such heading`)
      }
    }
  }
}

// The sidebar decides what a reader can reach. A page missing from it is
// only reachable by guessing the URL.
const registry = readFileSync(resolve(here, "..", "src", "docs", "registry.ts"), "utf8")
for (const slug of slugs) {
  if (!registry.includes(`"${slug}"`)) {
    problems.push(`registry.ts does not list ${slug}.md, so nothing links to it`)
  }
}

if (problems.length > 0) {
  console.error("Broken documentation links:\n")
  for (const p of problems) console.error(`  ${p}`)
  process.exit(1)
}
console.log(`docs: ${files.length} pages, every internal link resolves`)
