import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

/** Section is one band of the page, with the shared gutter and rhythm. */
export function Section({
  id,
  children,
  className,
}: {
  id?: string
  children: ReactNode
  className?: string
}) {
  return (
    <section id={id} className={cn("scroll-mt-14 border-t border-border/60 py-20 sm:py-28", className)}>
      <div className="mx-auto w-full max-w-6xl px-6">{children}</div>
    </section>
  )
}

/** Eyebrow is the small label above a section heading. */
export function Eyebrow({ children }: { children: ReactNode }) {
  return (
    <p className="mb-4 font-mono text-xs uppercase tracking-[0.18em] text-muted-foreground">
      {children}
    </p>
  )
}

/** Heading is a section title. */
export function Heading({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <h2
      className={cn(
        "text-balance text-3xl font-medium tracking-tight sm:text-4xl",
        className,
      )}
    >
      {children}
    </h2>
  )
}

/** Lede is the paragraph under a section title. */
export function Lede({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p className={cn("mt-4 max-w-2xl text-pretty text-base leading-relaxed text-muted-foreground sm:text-lg", className)}>
      {children}
    </p>
  )
}
