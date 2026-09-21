import { ClockAlertIcon, ServerIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

import { Panel } from '@/components/Panel'
import { Failure, Nothing, PageLoading } from '@/components/states'
import { SeverityTag, StatusDot } from '@/components/status'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/api'
import { relativeTime } from '@/format'
import { useLoad, useNow } from '@/hooks'
import type { Counts, RunnerStatus } from '@/types'

const POLL_MS = 5000

const STATS: Array<{ key: keyof Counts; label: string; status: RunnerStatus }> = [
  { key: 'total', label: 'Runners', status: 'offline' },
  { key: 'online', label: 'Online', status: 'online' },
  { key: 'busy', label: 'Busy', status: 'busy' },
  { key: 'offline', label: 'Offline', status: 'offline' },
  { key: 'error', label: 'Errored', status: 'error' },
]

export function Overview() {
  const { data, error, initial } = useLoad(() => api.overview(), POLL_MS)
  const now = useNow()

  if (initial) return <PageLoading />
  if (error) return <Failure error={error} />
  if (!data) return null

  const { runners, recent_events: events, thresholds } = data

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {STATS.map((stat) => (
          <Card key={stat.key} className="gap-0 py-4">
            <CardContent className="px-4">
              <div className="text-2xl leading-none">{runners[stat.key]}</div>
              <div className="mt-2 flex items-center gap-1.5 text-xs tracking-wide text-muted-foreground uppercase">
                {/* The total is a count, not a state, so it gets no dot. */}
                {stat.key !== 'total' && <StatusDot status={stat.status} />}
                {stat.label}
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {runners.stale > 0 && (
        <Alert>
          <ClockAlertIcon className="text-warn" />
          <AlertTitle>
            {runners.stale} runner{runners.stale === 1 ? '' : 's'}{' '}
            {runners.stale === 1 ? 'has' : 'have'} not reported in over{' '}
            {thresholds.stale_after_seconds}s
          </AlertTitle>
          <AlertDescription>
            {runners.stale === 1 ? 'It is' : 'They are'} still counted as up until{' '}
            {thresholds.offline_after_seconds}s.
          </AlertDescription>
        </Alert>
      )}

      {runners.retired > 0 && (
        <p className="text-sm text-muted-foreground">
          {runners.retired} runner{runners.retired === 1 ? ' has' : 's have'} retired. Ephemeral
          runners retire after their job, so this counts completed runs rather than problems.{' '}
          <Link to="/runners?retired=true" className="text-foreground underline underline-offset-2">
            Show them
          </Link>
          .
        </p>
      )}

      {runners.total === 0 ? (
        <Card>
          <CardContent>
            <Nothing
              title="No machine has enrolled with this control plane"
              hint="A machine enrolls with a token, then keeps reporting on its own. Issue one from Settings and run the two commands it gives you."
            >
              <Button asChild variant="outline" size="sm">
                <Link to="/settings">
                  <ServerIcon />
                  Issue an enrollment token
                </Link>
              </Button>
            </Nothing>
          </CardContent>
        </Card>
      ) : null}

      <Panel title="Recent events">
        {events.length === 0 ? (
          <Nothing
            title="Nothing has happened yet"
            hint="Events appear here as soon as an agent enrolls and starts reporting."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Severity</TableHead>
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
                  <TableCell className="font-mono text-xs">{event.event}</TableCell>
                  <TableCell className="text-muted-foreground">{event.message ?? ''}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>
    </div>
  )
}
