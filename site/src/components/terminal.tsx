import { useState, type ReactNode } from "react"
import { Check, Copy } from "lucide-react"

import { cn } from "@/lib/utils"

/** Terminal frames a block of real CLI output. */
export function Terminal({
  title,
  children,
  className,
}: {
  title?: string
  children: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        "min-w-0 overflow-hidden rounded-xl border border-border bg-card shadow-2xl shadow-black/40",
        className,
      )}
    >
      <div className="flex items-center gap-2 border-b border-border/70 px-4 py-2.5">
        <div className="flex gap-1.5" aria-hidden>
          <span className="size-2.5 rounded-full bg-muted-foreground/30" />
          <span className="size-2.5 rounded-full bg-muted-foreground/20" />
          <span className="size-2.5 rounded-full bg-muted-foreground/10" />
        </div>
        {title ? (
          <span className="ml-1.5 font-mono text-xs text-muted-foreground">{title}</span>
        ) : null}
      </div>
      <pre className="overflow-x-auto px-4 py-4 font-mono text-[12.5px] leading-relaxed sm:px-5 sm:text-[13px]">
        {children}
      </pre>
    </div>
  )
}

/** Prompt is a typed command line inside a Terminal. */
export function Prompt({ children }: { children: ReactNode }) {
  return (
    <div className="text-foreground">
      <span className="select-none text-muted-foreground">$ </span>
      {children}
    </div>
  )
}

/** Out is program output: dimmer than what the operator typed. */
export function Out({ children, dim }: { children: ReactNode; dim?: boolean }) {
  return <div className={dim ? "text-muted-foreground/60" : "text-muted-foreground"}>{children}</div>
}

/** Ok is a passing check line. Monochrome, so the mark carries the meaning. */
export function Ok({ children }: { children: ReactNode }) {
  return (
    <div className="text-foreground">
      <span className="select-none text-foreground/90">✓ </span>
      {children}
    </div>
  )
}

/** Bad is a failing check line. */
export function Bad({ children }: { children: ReactNode }) {
  return (
    <div className="text-foreground">
      <span className="select-none">✗ </span>
      {children}
    </div>
  )
}

/** CopyLine is a command the reader is meant to run, with a copy button. */
export function CopyLine({ command, className }: { command: string; className?: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      // Clipboard access can be refused. The command is selectable either
      // way, so there is nothing useful to say about it.
    }
  }

  return (
    <div
      className={cn(
        "group flex items-start gap-3 rounded-lg border border-border bg-card/60 py-3 pl-4 pr-3 backdrop-blur",
        className,
      )}
    >
      <span aria-hidden className="select-none pt-px font-mono text-[13px] leading-relaxed text-muted-foreground sm:text-sm">
        $
      </span>
      <code className="flex-1 break-all text-left font-mono text-[13px] leading-relaxed text-foreground sm:text-sm">
        {command}
      </code>
      <button
        type="button"
        onClick={copy}
        aria-label={copied ? "Copied" : `Copy: ${command}`}
        className="grid size-8 shrink-0 place-items-center rounded-md border border-transparent text-muted-foreground transition-colors hover:border-border hover:text-foreground focus-visible:border-border focus-visible:text-foreground focus-visible:outline-none"
      >
        {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
      </button>
    </div>
  )
}
