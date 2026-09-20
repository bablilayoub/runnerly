import { Link } from 'react-router-dom'
import { api } from '../api'
import { useLoad, useNow } from '../hooks'
import { relativeTime } from '../format'
import { Card, Empty, Failure, SeverityTag, Spinner, Stat } from '../components/primitives'
import { Cell, Row, Table } from '../components/Table'

const POLL_MS = 5000

export function Overview() {
  const { data, error, initial } = useLoad(() => api.overview(), POLL_MS)
  const now = useNow()

  if (initial) return <Spinner />
  if (error) return <Failure error={error} />
  if (!data) return null

  const { runners, recent_events: events, thresholds } = data

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <Stat label="Runners" value={runners.total} />
        <Stat label="Online" value={runners.online} />
        <Stat label="Busy" value={runners.busy} />
        <Stat label="Offline" value={runners.offline} />
        <Stat label="Errored" value={runners.error} />
      </div>

      {runners.retired > 0 && (
        <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
          {runners.retired} runner{runners.retired === 1 ? ' has' : 's have'} retired. Ephemeral
          runners retire after their job, so this counts completed runs rather than problems.{' '}
          <Link to="/runners?retired=true" className="underline">
            Show them
          </Link>
          .
        </p>
      )}

      {runners.stale > 0 && (
        <p className="text-sm" style={{ color: 'var(--warn)' }}>
          {runners.stale} runner{runners.stale === 1 ? '' : 's'} have not reported in over{' '}
          {thresholds.stale_after_seconds}s. They are still counted as up until{' '}
          {thresholds.offline_after_seconds}s.
        </p>
      )}

      <Card title="Recent events">
        {events.length === 0 ? (
          <Empty
            title="Nothing has happened yet"
            hint="Events appear here as soon as an agent enrolls and starts reporting."
          />
        ) : (
          <Table head={['When', 'Severity', 'Event', 'Message']}>
            {events.map((event) => (
              <Row key={event.id}>
                <Cell muted>{relativeTime(event.created_at, now)}</Cell>
                <Cell>
                  <SeverityTag severity={event.severity} />
                </Cell>
                <Cell mono>{event.event}</Cell>
                <Cell muted>{event.message ?? ''}</Cell>
              </Row>
            ))}
          </Table>
        )}
      </Card>

      {runners.total === 0 && (
        <Card title="No runners yet">
          <Empty
            title="No machine has enrolled with this control plane"
            hint={
              <>
                Issue a token with{' '}
                <code className="font-mono">runnerly server enrollment-token create</code>, then run{' '}
                <code className="font-mono">runnerly agent run</code> on the machine. See{' '}
                <Link to="/settings" className="underline">
                  Settings
                </Link>
                .
              </>
            }
          />
        </Card>
      )}
    </div>
  )
}
