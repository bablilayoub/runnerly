import type { ReactNode } from 'react'
import { OctagonAlertIcon } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

/**
 * Loading is a skeleton of the shape that is coming, not a spinner.
 *
 * A dashboard that polls shows this once, on the first load. Everything
 * after that replaces data in place, because a page that blanks every five
 * seconds is harder to read than one that updates quietly.
 */
export function Loading({ rows = 4, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn('space-y-3 p-4', className)} role="status" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-4">
          <Skeleton className="h-4 w-1/4" />
          <Skeleton className="h-4 w-1/6" />
          <Skeleton className="h-4 flex-1" />
        </div>
      ))}
    </div>
  )
}

/** PageLoading fills the whole page before anything is known. */
export function PageLoading() {
  return (
    <div className="space-y-4" role="status" aria-label="Loading">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {Array.from({ length: 5 }, (_, i) => (
          <Skeleton key={i} className="h-20" />
        ))}
      </div>
      <Skeleton className="h-64" />
    </div>
  )
}

/**
 * Failure shows a server error together with the hint it came with.
 *
 * Runnerly's API attaches a hint to the errors worth acting on, and
 * dropping it here would throw away the half that says what to do.
 */
export function Failure({ error }: { error: Error }) {
  const hint = 'hint' in error ? String(error.hint) : ''
  return (
    <Alert variant="destructive" role="alert">
      <OctagonAlertIcon />
      <AlertTitle>{error.message}</AlertTitle>
      {hint && <AlertDescription>{hint}</AlertDescription>}
    </Alert>
  )
}

/** Nothing says what to do, not only that there is nothing here. */
export function Nothing({
  title,
  hint,
  children,
}: {
  title: string
  hint?: ReactNode
  children?: ReactNode
}) {
  return (
    <Empty className="border-0 py-12">
      <EmptyHeader>
        <EmptyTitle className="text-base font-normal">{title}</EmptyTitle>
        {hint && <EmptyDescription className="text-balance">{hint}</EmptyDescription>}
      </EmptyHeader>
      {children}
    </Empty>
  )
}
