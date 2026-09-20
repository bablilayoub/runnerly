import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { useLoad, useNow } from '../hooks'
import { bytes, duration, orDash, percent, relativeTime } from '../format'
import {
  Button,
  Card,
  Empty,
  Failure,
  Label,
  SeverityTag,
  Spinner,
  Status,
} from '../components/primitives'
import { Cell, Row, Table } from '../components/Table'
import { Confirm } from '../components/Confirm'

const POLL_MS = 5000

export function RunnerDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const now = useNow()

  const runner = useLoad(() => api.runner(id), POLL_MS)
  const events = useLoad(() => api.events({ runnerId: id, limit: 50 }), POLL_MS)
  const commands = useLoad(() => api.commands(id), POLL_MS)

  const [confirming, setConfirming] = useState<'restart' | 'remove' | null>(null)
  const [notice, setNotice] = useState<string>()
  const [actionError, setActionError] = useState<Error>()

  async function restart() {
    setConfirming(null)
    setActionError(undefined)
    try {
      const result = await api.restartRunner(id)
      setNotice(result.note)
      commands.reload()
    } catch (err) {
      setActionError(err instanceof Error ? err : new Error(String(err)))
    }
  }

  async function remove() {
    setConfirming(null)
    setActionError(undefined)
    try {
      await api.deleteRunner(id)
      navigate('/runners')
    } catch (err) {
      setActionError(err instanceof Error ? err : new Error(String(err)))
    }
  }

  if (runner.initial) return <Spinner />
  if (runner.error) return <Failure error={runner.error} />
  if (!runner.data) return null

  const r = runner.data
  const pending = (commands.data?.commands ?? []).filter(
    (c) => c.status === 'pending' || c.status === 'delivered',
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <Link to="/runners" className="text-sm" style={{ color: 'var(--text-muted)' }}>
            ← Runners
          </Link>
          <h1 className="mt-1 text-xl font-medium">{r.name}</h1>
          <div className="mt-1 flex items-center gap-3 text-sm">
            <Status status={r.status} health={r.health} />
            <span style={{ color: 'var(--text-muted)' }}>{r.github_scope_id}</span>
          </div>
          {r.status_detail && (
            <p className="mt-1 text-sm" style={{ color: 'var(--text-muted)' }}>
              {r.status_detail}
            </p>
          )}
          {r.retired_at && (
            <p className="mt-1 text-sm" style={{ color: 'var(--text-muted)' }}>
              Retired {relativeTime(r.retired_at, now)}
              {r.retired_reason ? `: ${r.retired_reason}` : ''}. An ephemeral runner retires when
              its job is done, so this is a finished run rather than a failure.
            </p>
          )}
        </div>

        <div className="flex gap-2">
          <Button
            onClick={() => setConfirming('restart')}
            // A retired runner has no agent left to collect the command.
            disabled={pending.length > 0 || Boolean(r.retired_at)}
            title={
              r.retired_at
                ? 'This runner has retired; there is no agent to restart'
                : pending.length > 0
                  ? 'A restart is already queued'
                  : undefined
            }
          >
            {pending.length > 0 ? 'Restart queued' : 'Restart'}
          </Button>
          <Button variant="danger" onClick={() => setConfirming('remove')}>
            Remove
          </Button>
        </div>
      </div>

      {actionError && <Failure error={actionError} />}
      {notice && (
        <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
          {notice}
        </p>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        <Card title="Machine">
          <dl className="divide-y text-sm" style={{ borderColor: 'var(--border)' }}>
            <Field name="Platform" value={`${orDash(r.os)}/${orDash(r.architecture)}`} />
            <Field name="CPU" value={r.cpu_count ? `${r.cpu_count} cores` : '—'} />
            <Field name="Memory" value={bytes(r.memory_bytes)} />
            <Field name="Disk" value={bytes(r.disk_bytes)} />
            <Field name="Runner" value={orDash(r.runner_version)} />
            <Field name="Agent" value={orDash(r.agent_version)} />
            <Field name="Ephemeral" value={r.ephemeral ? 'yes' : 'no'} />
          </dl>
        </Card>

        <Card title="Reporting">
          <dl className="divide-y text-sm" style={{ borderColor: 'var(--border)' }}>
            <Field
              name="Last heartbeat"
              value={
                r.last_heartbeat_age_seconds === null
                  ? 'never'
                  : `${duration(r.last_heartbeat_age_seconds)} ago`
              }
            />
            <Field name="CPU load" value={percent(r.cpu_percent)} />
            <Field name="Memory used" value={percent(r.memory_percent)} />
            <Field name="Disk used" value={percent(r.disk_percent)} />
            <Field name="Enrolled" value={relativeTime(r.created_at, now)} />
            <Field name="GitHub host" value={r.github_host} />
          </dl>
          <p className="px-4 py-3 text-xs" style={{ color: 'var(--text-faint)' }}>
            Load figures are only as fresh as the last heartbeat, and a dash means the agent has not
            measured that yet.
          </p>
        </Card>
      </div>

      <Card title="Labels">
        {r.labels.length === 0 ? (
          <Empty title="No labels" />
        ) : (
          <div className="flex flex-wrap gap-1.5 px-4 py-3">
            {r.labels.map((label) => (
              <Label key={label}>{label}</Label>
            ))}
          </div>
        )}
      </Card>

      {(commands.data?.commands.length ?? 0) > 0 && (
        <Card title="Commands">
          <Table head={['When', 'Command', 'Status', 'By', 'Detail']}>
            {commands.data!.commands.map((c) => (
              <Row key={c.id}>
                <Cell muted>{relativeTime(c.created_at, now)}</Cell>
                <Cell mono>{c.command}</Cell>
                <Cell muted={c.status !== 'failed'} {...(c.status === 'failed' ? {} : {})}>
                  <span style={c.status === 'failed' ? { color: 'var(--bad)' } : undefined}>
                    {c.status}
                  </span>
                </Cell>
                <Cell muted>{orDash(c.requested_by)}</Cell>
                <Cell muted>{orDash(c.error)}</Cell>
              </Row>
            ))}
          </Table>
        </Card>
      )}

      <Card title="Events">
        {events.initial ? (
          <Spinner />
        ) : (events.data?.events.length ?? 0) === 0 ? (
          <Empty title="No events for this runner yet" />
        ) : (
          <Table head={['When', 'Severity', 'Event', 'Message']}>
            {events.data!.events.map((event) => (
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

      {confirming === 'restart' && (
        <Confirm
          title={`Restart ${r.name}?`}
          body={
            <>
              The agent stops the runner and starts it again. It sends SIGTERM first, so a job
              already running gets to finish.
              <br />
              <br />
              This is queued, not immediate: the agent collects it on its next heartbeat.
            </>
          }
          confirmLabel="Restart"
          onConfirm={() => void restart()}
          onCancel={() => setConfirming(null)}
        />
      )}

      {confirming === 'remove' && (
        <Confirm
          title={`Remove ${r.name}?`}
          danger
          body={
            <>
              This forgets the runner here. It does <strong>not</strong> deregister it with GitHub
              and does not touch the machine, so it will reappear the next time its agent sends a
              heartbeat.
              <br />
              <br />
              To retire it for good, run{' '}
              <code className="font-mono">runnerly runner remove {r.name} --purge</code> on the
              machine first.
            </>
          }
          confirmLabel="Remove"
          onConfirm={() => void remove()}
          onCancel={() => setConfirming(null)}
        />
      )}
    </div>
  )
}

function Field({ name, value }: { name: string; value: string }) {
  return (
    <div className="flex justify-between gap-4 px-4 py-2" style={{ borderColor: 'var(--border)' }}>
      <dt style={{ color: 'var(--text-muted)' }}>{name}</dt>
      <dd className="text-right">{value}</dd>
    </div>
  )
}
