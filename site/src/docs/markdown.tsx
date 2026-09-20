import type { ComponentPropsWithoutRef, ReactNode } from "react"
import { Link } from "react-router-dom"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"

import { slugify } from "@/docs/registry"
import { cn } from "@/lib/utils"

/**
 * Renders a documentation page.
 *
 * Code blocks are not syntax highlighted, and that is the point rather than
 * an omission: the palette has no hue, and most of these blocks are terminal
 * output where color would imply meaning the output does not carry. Runnerly
 * uses ✓ and ✗ for the same reason.
 */
export function Markdown({ markdown }: { markdown: string }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        h1: () => null, // The page header renders the title itself.
        h2: (p) => <Anchored as="h2" {...p} />,
        h3: (p) => <Anchored as="h3" {...p} />,
        h4: (p) => <Anchored as="h4" {...p} />,

        p: ({ children }) => (
          <p className="mb-5 leading-[1.75] text-muted-foreground">{children}</p>
        ),

        a: ({ href, children }) => <DocLink href={href}>{children}</DocLink>,

        ul: ({ children }) => (
          <ul className="mb-5 ml-5 list-disc space-y-2 leading-[1.75] text-muted-foreground marker:text-muted-foreground/40">
            {children}
          </ul>
        ),
        ol: ({ children }) => (
          <ol className="mb-5 ml-5 list-decimal space-y-2 leading-[1.75] text-muted-foreground marker:text-muted-foreground/40">
            {children}
          </ol>
        ),
        li: ({ children }) => <li className="pl-1.5">{children}</li>,

        strong: ({ children }) => (
          <strong className="font-medium text-foreground">{children}</strong>
        ),

        blockquote: ({ children }) => (
          <blockquote className="mb-5 border-l-2 border-border py-1 pl-5 [&>p:last-child]:mb-0">
            {children}
          </blockquote>
        ),

        hr: () => <hr className="my-10 border-border" />,

        code: ({ className, children, ...rest }: ComponentPropsWithoutRef<"code">) => {
          // react-markdown gives a fenced block a language class and an
          // inline span none, which is the only way to tell them apart here.
          const fenced = /language-/.test(className ?? "")
          if (fenced) {
            return (
              <code className={cn("font-mono text-[13px] leading-[1.7]", className)} {...rest}>
                {children}
              </code>
            )
          }
          return (
            <code className="rounded border border-border/70 bg-card px-[0.35em] py-[0.1em] font-mono text-[0.85em] text-foreground">
              {children}
            </code>
          )
        },

        pre: ({ children }) => (
          <pre className="mb-6 overflow-x-auto rounded-xl border border-border bg-card px-4 py-3.5 sm:px-5">
            {children}
          </pre>
        ),

        table: ({ children }) => (
          <div className="mb-6 overflow-x-auto rounded-xl border border-border">
            <table className="w-full border-collapse text-sm">{children}</table>
          </div>
        ),
        thead: ({ children }) => <thead className="bg-card/70">{children}</thead>,
        th: ({ children }) => (
          <th className="border-b border-border px-4 py-2.5 text-left font-medium">{children}</th>
        ),
        td: ({ children }) => (
          <td className="border-b border-border/50 px-4 py-2.5 align-top text-muted-foreground">
            {children}
          </td>
        ),

        img: ({ src, alt }) => (
          <img
            src={typeof src === "string" ? src : undefined}
            alt={alt}
            className="mb-6 rounded-xl border border-border"
          />
        ),
      }}
    >
      {markdown}
    </ReactMarkdown>
  )
}

/** A heading you can link to, with the same anchor GitHub would give it. */
function Anchored({ as: Tag, children }: { as: "h2" | "h3" | "h4"; children?: ReactNode }) {
  const id = slugify(textOf(children))
  const size = {
    h2: "mt-12 mb-4 text-2xl font-medium tracking-tight",
    h3: "mt-9 mb-3 text-lg font-medium tracking-tight",
    h4: "mt-7 mb-2 text-base font-medium tracking-tight",
  }[Tag]

  return (
    <Tag id={id} className={cn("group scroll-mt-24 text-foreground", size)}>
      <a href={`#${id}`} className="no-underline">
        {children}
        <span
          aria-hidden
          className="ml-2 select-none text-muted-foreground/0 transition-colors group-hover:text-muted-foreground/50"
        >
          #
        </span>
      </a>
    </Tag>
  )
}

/**
 * Links inside the docs point at each other the way they do in the
 * repository: `[the agent](agent.md)`. On the site those become routes, so
 * a reader never leaves for GitHub just to follow a cross-reference.
 */
function DocLink({ href, children }: { href?: string; children: ReactNode }) {
  const style =
    "underline decoration-muted-foreground/30 underline-offset-[3px] transition-colors hover:decoration-foreground"

  if (!href) return <span>{children}</span>

  // ../README.md and other escapes out of docs/ have no page here.
  const internal = /^[\w.-]+\.md(#.*)?$/.exec(href)
  if (internal) {
    const [file, hash = ""] = href.split("#")
    const slug = file.replace(/\.md$/, "")
    return (
      <Link to={`/docs/${slug}${hash ? `#${hash}` : ""}`} className={cn(style, "text-foreground")}>
        {children}
      </Link>
    )
  }

  if (href.startsWith("#")) {
    return (
      <a href={href} className={cn(style, "text-foreground")}>
        {children}
      </a>
    )
  }

  const external = /^https?:/.test(href)
  return (
    <a
      href={external ? href : repoURL(href)}
      target="_blank"
      rel="noreferrer"
      className={cn(style, "text-foreground")}
    >
      {children}
    </a>
  )
}

/** A relative link to something that is not a docs page still resolves. */
function repoURL(href: string): string {
  const clean = href.replace(/^(\.\.\/)+/, "")
  return `https://github.com/bablilayoub/runnerly/blob/main/${clean}`
}

function textOf(node: ReactNode): string {
  if (node == null || typeof node === "boolean") return ""
  if (typeof node === "string" || typeof node === "number") return String(node)
  if (Array.isArray(node)) return node.map(textOf).join("")
  if (typeof node === "object" && "props" in node) {
    return textOf((node as { props: { children?: ReactNode } }).props.children)
  }
  return ""
}
