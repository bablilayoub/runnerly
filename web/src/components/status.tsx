import type { ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Health, RunnerStatus, Severity } from '@/types'

/**
 * Runner status and event severity are the only things on this page with a
 * hue. That is the point: when everything else is grey, the eye finds the
 * one runner that is wrong without being told where to look.
 *
 * Colour never carries the meaning alone. Every dot sits next to its word,
 * so the page works in greyscale and for a reader who cannot tell the
 * green from the red.
 */
const STATUS_COLOR: Record<RunnerStatus, string> = {
  online: 'bg-ok',
  starting: 'bg-ok',
  busy: 'bg-busy',
  error: 'bg-bad',
  // Retired and offline are both quiet: a retired runner finished on
  // purpose and is not a problem to look at.
  stopping: 'bg-muted-foreground',
  offline: 'bg-muted-foreground',
  retired: 'bg-muted-foreground',
}

export function StatusDot({ status, className }: { status: RunnerStatus; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn('inline-block size-2 shrink-0 rounded-full', STATUS_COLOR[status], className)}
    />
  )
}

/** Status pairs the dot with its word, so the meaning is not in the hue. */
export function Status({ status, health }: { status: RunnerStatus; health?: Health }) {
  return (
    <span className="inline-flex items-center gap-2 whitespace-nowrap">
      <StatusDot status={status} />
      <span className="text-sm">{status}</span>
      {/* A retired runner is already offline by definition, so saying it is
          also stale would be noise. */}
      {health === 'stale' && status !== 'retired' && (
        <Badge variant="outline" className="border-warn/40 text-warn">
          stale
        </Badge>
      )}
    </span>
  )
}

const SEVERITY_COLOR: Record<Severity, string> = {
  error: 'text-bad',
  warn: 'text-warn',
  info: 'text-muted-foreground',
  debug: 'text-muted-foreground/70',
}

export function SeverityTag({ severity }: { severity: Severity }) {
  return (
    <span className={cn('font-mono text-xs uppercase', SEVERITY_COLOR[severity])}>{severity}</span>
  )
}

/** LabelTag is one of a runner's GitHub labels. */
export function LabelTag({ children }: { children: ReactNode }) {
  return (
    <Badge variant="outline" className="font-mono text-muted-foreground">
      {children}
    </Badge>
  )
}
