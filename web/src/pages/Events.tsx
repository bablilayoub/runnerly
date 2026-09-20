import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { useLoad, useNow } from '../hooks'
import { relativeTime } from '../format'
import { Card, Empty, Failure, SeverityTag, Spinner } from '../components/primitives'
import { Cell, Row, Table } from '../components/Table'
import type { Severity } from '../types'

const POLL_MS = 5000
const SEVERITIES: Array<Severity | ''> = ['', 'info', 'warn', 'error']

export function Events() {
  const [severity, setSeverity] = useState<Severity | ''>('')
  const { data, error, initial } = useLoad(
    () => api.events(severity ? { severity, limit: 200 } : { limit: 200 }),
    POLL_MS,
  )
  const now = useNow()

  const events = data?.events ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs tracking-wide uppercase" style={{ color: 'var(--text-muted)' }}>
          Severity
        </span>
        {SEVERITIES.map((value) => (
          <button
            key={value || 'all'}
            onClick={() => setSeverity(value)}
            className="cursor-pointer rounded-sm border px-2 py-1 text-xs"
            style={{
              borderColor: severity === value ? 'var(--text)' : 'var(--border)',
              color: severity === value ? 'var(--text)' : 'var(--text-muted)',
            }}
          >
            {value || 'all'}
          </button>
        ))}
      </div>

      {error ? (
        <Failure error={error} />
      ) : (
        <Card title={`Events (${events.length})`}>
          {initial ? (
            <Spinner />
          ) : events.length === 0 ? (
            <Empty
              title="No events match"
              hint="The feed is pruned on a schedule, so older history will not be here."
            />
          ) : (
            <Table head={['When', 'Severity', 'Runner', 'Event', 'Message']}>
              {events.map((event) => (
                <Row key={event.id}>
                  <Cell muted>{relativeTime(event.created_at, now)}</Cell>
                  <Cell>
                    <SeverityTag severity={event.severity} />
                  </Cell>
                  <Cell mono muted>
                    {event.runner_id ? (
                      <Link to={`/runners/${event.runner_id}`} className="underline">
                        {event.runner_id.slice(0, 8)}
                      </Link>
                    ) : (
                      '—'
                    )}
                  </Cell>
                  <Cell mono>{event.event}</Cell>
                  <Cell muted>{event.message ?? ''}</Cell>
                </Row>
              ))}
            </Table>
          )}
        </Card>
      )}
    </div>
  )
}
