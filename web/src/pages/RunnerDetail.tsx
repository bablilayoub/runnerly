import { useState } from 'react'
import { ArrowLeftIcon, RotateCwIcon, Trash2Icon } from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { LoadMeter } from '@/components/LoadMeter'
import { Panel } from '@/components/Panel'
import { Failure, Loading, Nothing, PageLoading } from '@/components/states'
import { LabelTag, SeverityTag, Status } from '@/components/status'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/api'
import { bytes, duration, orDash, relativeTime } from '@/format'
import { useLoad, useNow } from '@/hooks'
import { cn } from '@/lib/utils'

const POLL_MS = 5000

export function RunnerDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const now = useNow()

  // Keyed on the id: an event on the events page links straight to a
  // different runner, and without this the page would show the previous
  // one's machine and load until the next poll.
  const runner = useLoad(() => api.runner(id), POLL_MS, [id])
  const events = useLoad(() => api.events({ runnerId: id, limit: 50 }), POLL_MS, [id])
  const commands = useLoad(() => api.commands(id), POLL_MS, [id])

  const [confirming, setConfirming] = useState<'restart' | 'remove' | null>(null)

  async function restart() {
    setConfirming(null)
    try {
      const result = await api.restartRunner(id)
      // The note explains that this is queued rather than immediate, which
      // is the part an operator needs to read before wondering why nothing
      // has happened yet.
      toast.success('Restart requested', { description: result.note })
      commands.reload()
    } catch (err) {
      toast.error('Could not request a restart', { description: message(err) })
    }
  }

  async function remove() {
    setConfirming(null)
    try {
      await api.deleteRunner(id)
      toast.success('Runner removed from the control plane')
      navigate('/runners')
    } catch (err) {
      toast.error('Could not remove the runner', { description: message(err) })
    }
  }

  if (runner.initial) return <PageLoading />
  if (runner.error) return <Failure error={runner.error} />
  if (!runner.data) return null

  const r = runner.data
  const pending = (commands.data?.commands ?? []).filter(
    (c) => c.status === 'pending' || c.status === 'delivered',
  )
  // A retired runner has no agent left to collect the command.
  const cannotRestart = r.retired_at
    ? 'This runner has retired; there is no agent to restart'
    : pending.length > 0
      ? 'A restart is already queued'
      : ''

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <Link
            to="/runners"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeftIcon className="size-3.5" />
            Runners
          </Link>
          <h1 className="mt-1 text-xl font-medium">{r.name}</h1>
          <div className="mt-1 flex flex-wrap items-center gap-3 text-sm">
            <Status status={r.status} health={r.health} />
            <span className="font-mono text-muted-foreground">{r.github_scope_id}</span>
          </div>
          {r.status_detail && (
            <p className="mt-1 text-sm text-muted-foreground">{r.status_detail}</p>
          )}
          {r.retired_at && (
            <p className="mt-1 max-w-prose text-sm text-muted-foreground">
              Retired {relativeTime(r.retired_at, now)}
              {r.retired_reason ? `: ${r.retired_reason}` : ''}. An ephemeral runner retires when
              its job is done, so this is a finished run rather than a failure.
            </p>
          )}
        </div>

        <div className="flex gap-2">
          <RestartButton
            reason={cannotRestart}
            queued={pending.length > 0}
            onClick={() => setConfirming('restart')}
          />
          <Button variant="destructive" size="sm" onClick={() => setConfirming('remove')}>
            <Trash2Icon />
            Remove
          </Button>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel title="Machine" bodyClassName="divide-y divide-border">
          <Field name="Platform" value={`${orDash(r.os)}/${orDash(r.architecture)}`} />
          <Field name="CPU" value={r.cpu_count ? `${r.cpu_count} cores` : '—'} />
          <Field name="Memory" value={bytes(r.memory_bytes)} />
          <Field name="Disk" value={bytes(r.disk_bytes)} />
          <Field name="Runner" value={orDash(r.runner_version)} mono />
          <Field name="Agent" value={orDash(r.agent_version)} mono />
          <Field name="Ephemeral" value={r.ephemeral ? 'yes' : 'no'} />
        </Panel>

        <Panel title="Reporting">
          <div className="space-y-4 px-4 py-4">
            <LoadMeter name="Processor" value={r.cpu_percent} />
            <LoadMeter name="Memory" value={r.memory_percent} />
            <LoadMeter name="Disk" value={r.disk_percent} />
          </div>
          <div className="divide-y divide-border border-t border-border">
            <Field
              name="Last heartbeat"
              value={
                r.last_heartbeat_age_seconds === null
                  ? 'never'
                  : `${duration(r.last_heartbeat_age_seconds)} ago`
              }
            />
            <Field name="Enrolled" value={relativeTime(r.created_at, now)} />
            <Field name="GitHub host" value={r.github_host} mono />
          </div>
          <p className="border-t border-border px-4 py-3 text-xs text-muted-foreground">
            Load figures are only as fresh as the last heartbeat, and a dash means the agent has no
            way to measure that one.
          </p>
        </Panel>
      </div>

      <Panel title="Labels">
        {r.labels.length === 0 ? (
          <Nothing title="No labels" />
        ) : (
          <div className="flex flex-wrap gap-1.5 px-4 py-3">
            {r.labels.map((label) => (
              <LabelTag key={label}>{label}</LabelTag>
            ))}
          </div>
        )}
      </Panel>

      {(commands.data?.commands.length ?? 0) > 0 && (
        <Panel title="Commands">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Command</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>By</TableHead>
                <TableHead>Detail</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {commands.data!.commands.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    {relativeTime(c.created_at, now)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{c.command}</TableCell>
                  <TableCell
                    className={cn(c.status === 'failed' ? 'text-bad' : 'text-muted-foreground')}
                  >
                    {c.status}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{orDash(c.requested_by)}</TableCell>
                  <TableCell className="text-muted-foreground">{orDash(c.error)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Panel>
      )}

      <Panel title="Events">
        {events.initial ? (
          <Loading rows={3} />
        ) : (events.data?.events.length ?? 0) === 0 ? (
          <Nothing title="No events for this runner yet" />
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
              {events.data!.events.map((event) => (
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

      {confirming === 'restart' && (
        <ConfirmDialog
          title={`Restart ${r.name}?`}
          body={
            <>
              <p>
                The agent stops the runner and starts it again. It sends SIGTERM first, so a job
                already running gets to finish.
              </p>
              <p>This is queued, not immediate: the agent collects it on its next heartbeat.</p>
            </>
          }
          confirmLabel="Restart"
          onConfirm={() => void restart()}
          onCancel={() => setConfirming(null)}
        />
      )}

      {confirming === 'remove' && (
        <ConfirmDialog
          title={`Remove ${r.name}?`}
          danger
          body={
            <>
              <p>
                This forgets the runner here. It does <strong>not</strong> deregister it with GitHub
                and does not touch the machine, so it will reappear the next time its agent sends a
                heartbeat.
              </p>
              <p>
                To retire it for good, run{' '}
                <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
                  runnerly runner remove {r.name} --purge
                </code>{' '}
                on the machine first.
              </p>
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

/**
 * A disabled button that does not say why is a dead end, and `title` only
 * appears for a mouse. The tooltip needs a live element to hang off, so
 * the button is wrapped rather than disabled outright.
 */
function RestartButton({
  reason,
  queued,
  onClick,
}: {
  reason: string
  queued: boolean
  onClick: () => void
}) {
  const button = (
    <Button variant="outline" size="sm" onClick={onClick} disabled={Boolean(reason)}>
      <RotateCwIcon />
      {queued ? 'Restart queued' : 'Restart'}
    </Button>
  )

  if (!reason) return button

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0}>{button}</span>
      </TooltipTrigger>
      <TooltipContent>{reason}</TooltipContent>
    </Tooltip>
  )
}

function Field({ name, value, mono }: { name: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4 px-4 py-2 text-sm">
      <dt className="text-muted-foreground">{name}</dt>
      <dd className={cn('text-right', mono && 'font-mono text-xs')}>{value}</dd>
    </div>
  )
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
