import type { ReactNode } from 'react'

import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

/**
 * Panel is a titled box: the shape every listing on this dashboard uses.
 *
 * The card's own padding is turned off because most of what goes in one is
 * a table, and a table needs to reach the edges — a listing inset by
 * sixteen pixels on both sides wastes the width the columns need on a
 * laptop.
 */
export function Panel({
  title,
  action,
  children,
  bodyClassName,
}: {
  title?: ReactNode
  action?: ReactNode
  children: ReactNode
  bodyClassName?: string
}) {
  return (
    <Card className="gap-0 py-0">
      {(title || action) && (
        <div className="flex min-h-11 items-center justify-between gap-3 border-b border-border px-4">
          <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            {title}
          </h2>
          {action}
        </div>
      )}
      <div className={cn('min-w-0', bodyClassName)}>{children}</div>
    </Card>
  )
}
