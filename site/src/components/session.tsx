import { useEffect, useRef, useState } from "react"
import { useInView, useReducedMotion } from "motion/react"

import { cn } from "@/lib/utils"

/**
 * A replay of a real terminal session.
 *
 * Runnerly is a command line tool, so the most honest thing the page can
 * show is the thing running. Every line here is copied from actual output,
 * and the replay only controls when each one appears.
 */

export type Line =
  /** A command the operator typed, revealed a character at a time. */
  | { kind: "cmd"; text: string }
  /** Program output. */
  | { kind: "out"; text: string; dim?: boolean }
  /** A passing check. */
  | { kind: "ok"; text: string }
  /** A blank line. */
  | { kind: "gap" }

/** How long each line waits before it appears. */
const PAUSE = { cmd: 420, out: 90, ok: 190, gap: 120 } as const
/** Milliseconds per typed character. */
const TYPE = 32

export function Session({
  title,
  lines,
  className,
  minLines = 0,
}: {
  title: string
  lines: Line[]
  className?: string
  /** Reserve this many rows, so the frame does not grow as it plays. */
  minLines?: number
}) {
  const frame = useRef<HTMLDivElement>(null)
  const inView = useInView(frame, { once: true, margin: "-120px" })
  const still = useReducedMotion()

  // Everything at once for reduced motion: the content is the point, the
  // replay is decoration.
  const [shown, setShown] = useState(() => (still ? lines.length : 0))
  const [typed, setTyped] = useState("")

  useEffect(() => {
    if (still || !inView) return
    if (shown >= lines.length) return

    const line = lines[shown]
    let cancelled = false
    const timers: number[] = []

    const advance = () => {
      if (!cancelled) setShown((n) => n + 1)
    }

    if (line.kind === "cmd") {
      // Type the command out, then commit it and move on.
      let i = 0
      const tick = () => {
        if (cancelled) return
        i += 1
        setTyped(line.text.slice(0, i))
        if (i < line.text.length) {
          timers.push(window.setTimeout(tick, TYPE))
        } else {
          timers.push(
            window.setTimeout(() => {
              setTyped("")
              advance()
            }, PAUSE.cmd),
          )
        }
      }
      timers.push(window.setTimeout(tick, 240))
    } else {
      timers.push(window.setTimeout(advance, PAUSE[line.kind]))
    }

    return () => {
      cancelled = true
      timers.forEach(window.clearTimeout)
    }
  }, [inView, shown, lines, still])

  const done = shown >= lines.length
  const typing = !done && lines[shown]?.kind === "cmd"
  const rows = Math.max(minLines, lines.length)

  return (
    <div
      ref={frame}
      className={cn(
        "min-w-0 overflow-hidden rounded-xl border border-border bg-card shadow-2xl shadow-black/50",
        className,
      )}
    >
      <div className="flex items-center gap-2 border-b border-border/70 px-4 py-2.5">
        <div className="flex gap-1.5" aria-hidden>
          <span className="size-2.5 rounded-full bg-muted-foreground/30" />
          <span className="size-2.5 rounded-full bg-muted-foreground/20" />
          <span className="size-2.5 rounded-full bg-muted-foreground/10" />
        </div>
        <span className="ml-1.5 font-mono text-xs text-muted-foreground">{title}</span>
      </div>

      <div
        className="overflow-x-auto px-4 py-4 font-mono text-[12.5px] leading-[1.75] sm:px-5 sm:text-[13px]"
        style={{ minHeight: `calc(${rows} * 1.75em + 2rem)` }}
        // The replay is decoration over text that is all present in the DOM;
        // a screen reader should get it in one piece, not a line at a time.
        aria-live="off"
      >
        {lines.slice(0, shown).map((line, i) => (
          <Row key={i} line={line} />
        ))}

        {typing ? (
          <div className="text-foreground">
            <span className="select-none text-muted-foreground">$ </span>
            {typed}
            <Caret />
          </div>
        ) : null}
      </div>
    </div>
  )
}

function Row({ line }: { line: Line }) {
  switch (line.kind) {
    case "gap":
      return <div>&nbsp;</div>
    case "cmd":
      return (
        <div className="text-foreground">
          <span className="select-none text-muted-foreground">$ </span>
          {line.text}
        </div>
      )
    case "ok":
      return (
        <div className="text-foreground">
          <span className="select-none">✓ </span>
          {line.text}
        </div>
      )
    case "out":
      return (
        <div className={line.dim ? "text-muted-foreground/55" : "text-muted-foreground"}>
          {line.text}
        </div>
      )
  }
}

function Caret() {
  return (
    <span
      aria-hidden
      className="ml-px inline-block h-[1em] w-[0.55em] translate-y-[0.15em] animate-pulse bg-foreground"
    />
  )
}
