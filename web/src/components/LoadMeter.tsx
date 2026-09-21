import { Progress } from '@/components/ui/progress'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

/**
 * LoadMeter shows one of the three utilization figures an agent reports.
 *
 * Zero means the reading was never taken — no platform reports a machine
 * as exactly 0.0% busy — so it renders as a dash with no bar at all,
 * rather than as an empty bar that would read as "idle".
 *
 * Above 90% the bar takes the warning hue. That is the one number on this
 * page where the value itself is the problem, and a full grey bar looks
 * identical to a full grey bar at 60%.
 */
export function LoadMeter({ name, value }: { name: string; value: number }) {
  const measured = value > 0

  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between gap-4 text-sm">
        <span className="text-muted-foreground">{name}</span>
        <span className={cn('tabular-nums', !measured && 'text-muted-foreground')}>
          {measured ? `${Math.round(value)}%` : '—'}
        </span>
      </div>
      {measured ? (
        <Progress
          value={Math.min(100, value)}
          aria-label={`${name}: ${Math.round(value)} percent`}
          className={cn('h-1.5', value >= 90 && '[&>*]:bg-warn')}
        />
      ) : (
        // A track with no fill, so the row keeps its height and the column
        // of bars stays a column.
        <div className="h-1.5 rounded-full bg-muted" />
      )}
    </div>
  )
}

/**
 * MiniLoad is the same three readings in the width of a table column:
 * processor, memory, disk, top to bottom.
 *
 * It is deliberately unlabelled. The point of it in a list is to make the
 * one busy machine visible while scanning, not to be read precisely — the
 * tooltip and the runner's own page are for that.
 */
export function MiniLoad({ cpu, memory, disk }: { cpu: number; memory: number; disk: number }) {
  const readings = [
    { name: 'CPU', value: cpu },
    { name: 'Memory', value: memory },
    { name: 'Disk', value: disk },
  ]
  const measured = readings.filter((r) => r.value > 0)

  if (measured.length === 0) {
    return <span className="text-muted-foreground">—</span>
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {/* Labelled rather than focusable: the tooltip is a convenience
            for a pointer, and a tab stop per row would put five of them
            between the reader and the next thing they wanted. The same
            numbers are read out here, and spelled out on the runner's
            own page. */}
        <span
          role="img"
          aria-label={readings
            .map((r) => `${r.name} ${r.value > 0 ? `${Math.round(r.value)} percent` : 'unknown'}`)
            .join(', ')}
          className="inline-flex w-14 flex-col gap-[3px] py-1 align-middle"
        >
          {readings.map((reading) => (
            <span key={reading.name} className="h-[3px] w-full rounded-full bg-muted">
              {reading.value > 0 && (
                <span
                  className={cn(
                    'block h-full rounded-full',
                    reading.value >= 90 ? 'bg-warn' : 'bg-foreground/60',
                  )}
                  style={{ width: `${Math.min(100, reading.value)}%` }}
                />
              )}
            </span>
          ))}
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <span className="font-mono text-xs">
          {readings
            .map((r) => `${r.name} ${r.value > 0 ? `${Math.round(r.value)}%` : '—'}`)
            .join('   ')}
        </span>
      </TooltipContent>
    </Tooltip>
  )
}
