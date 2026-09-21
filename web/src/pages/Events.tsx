import { useState } from 'react'
import { Link } from 'react-router-dom'

import { Panel } from '@/components/Panel'
import { Failure, Loading, Nothing } from '@/components/states'
import { SeverityTag } from '@/components/status'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api } from '@/api'
import { relativeTime } from '@/format'
import { useLoad, useNow } from '@/hooks'
import type { Severity } from '@/types'

const POLL_MS = 5000
const SEVERITIES: Array<{ value: Severity | 'all'; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
]

export function Events() {
  const [severity, setSeverity] = useState<Severity | 'all'>('all')
  const { data, error, initial } = useLoad(
    () => api.events(severity === 'all' ? { limit: 200 } : { severity, limit: 200 }),
    POLL_MS,
    [severity],
  )
  const now = useNow()

  const events = data?.events ?? []

  if (error) return <Failure error={error} />

  return (
    <Panel
      title={initial ? 'Events' : `Events (${events.length})`}
      action={
        <ToggleGroup
          type="single"
          size="sm"
          value={severity}
          // A toggle group can be emptied by pressing the active item.
          // Falling back to "all" keeps the list from going blank with no
          // filter visibly selected.
          onValueChange={(value) => setSeverity((value || 'all') as Severity | 'all')}
          aria-label="Filter by severity"
        >
          {SEVERITIES.map((item) => (
            <ToggleGroupItem key={item.value} value={item.value} className="px-2.5 text-xs">
              {item.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      }
    >
      {initial ? (
        <Loading rows={6} />
      ) : events.length === 0 ? (
        <Nothing
          title="No events match"
          hint="The feed is pruned on a schedule, so older history will not be here."
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>When</TableHead>
              <TableHead>Severity</TableHead>
              <TableHead>Runner</TableHead>
              <TableHead>Event</TableHead>
              <TableHead>Message</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.map((event) => (
              <TableRow key={event.id}>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  {relativeTime(event.created_at, now)}
                </TableCell>
                <TableCell>
                  <SeverityTag severity={event.severity} />
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {event.runner_id ? (
                    <Link
                      to={`/runners/${event.runner_id}`}
                      className="underline underline-offset-2 hover:text-foreground"
                    >
                      {event.runner_id.slice(0, 8)}
                    </Link>
                  ) : (
                    '—'
                  )}
                </TableCell>
                <TableCell className="font-mono text-xs">{event.event}</TableCell>
                <TableCell className="text-muted-foreground">{event.message ?? ''}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  )
}
