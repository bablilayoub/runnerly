import { useNavigate, useSearchParams } from 'react-router-dom'

import { MiniLoad } from '@/components/LoadMeter'
import { Panel } from '@/components/Panel'
import { Failure, Loading, Nothing } from '@/components/states'
import { LabelTag, Status } from '@/components/status'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/api'
import { duration, orDash, relativeTime } from '@/format'
import { useLoad, useNow } from '@/hooks'

const POLL_MS = 5000

export function Runners() {
  const [params, setParams] = useSearchParams()
  // Ephemeral runners retire constantly, so finished ones are hidden until
  // asked for. Otherwise a machine doing its job all day buries the runners
  // that are actually in service.
  const includeRetired = params.get('retired') === 'true'

  const { data, error, initial } = useLoad(() => api.runners(includeRetired), POLL_MS)
  const navigate = useNavigate()
  const now = useNow() // keep the heartbeat ages moving between polls

  if (error) return <Failure error={error} />

  const runners = data?.runners ?? []

  return (
    <Panel
      title={initial ? 'Runners' : `Runners (${runners.length})`}
      action={
        <div className="flex items-center gap-2">
          <Checkbox
            id="include-retired"
            checked={includeRetired}
            onCheckedChange={(checked) => {
              const next = new URLSearchParams(params)
              if (checked === true) next.set('retired', 'true')
              else next.delete('retired')
              setParams(next, { replace: true })
            }}
          />
          <Label htmlFor="include-retired" className="text-xs font-normal text-muted-foreground">
            Include retired
          </Label>
        </div>
      }
    >
      {initial ? (
        <Loading rows={5} />
      ) : runners.length === 0 ? (
        <Nothing
          title={includeRetired ? 'No runners at all' : 'No runners in service'}
          hint={
            includeRetired
              ? 'This lists what the control plane knows about, which is not the same as what GitHub has registered.'
              : 'Finished ephemeral runners are hidden. Tick "include retired" to see them.'
          }
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Scope</TableHead>
              <TableHead>Platform</TableHead>
              <TableHead>Load</TableHead>
              <TableHead>Labels</TableHead>
              <TableHead>Heartbeat</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {runners.map((runner) => (
              <TableRow
                key={runner.id}
                onClick={() => navigate(`/runners/${runner.id}`)}
                className="cursor-pointer"
              >
                <TableCell>
                  <span className="font-medium whitespace-nowrap">{runner.name}</span>
                  {runner.ephemeral && (
                    <span className="ml-2 text-xs text-muted-foreground">ephemeral</span>
                  )}
                </TableCell>
                <TableCell>
                  <Status status={runner.status} health={runner.health} />
                </TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {runner.github_scope_id}
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  {orDash(runner.os)}/{orDash(runner.architecture)}
                </TableCell>
                <TableCell>
                  <MiniLoad
                    cpu={runner.cpu_percent}
                    memory={runner.memory_percent}
                    disk={runner.disk_percent}
                  />
                </TableCell>
                <TableCell>
                  {/* One line, always. Wrapping these turned every row into
                      four, and a list whose rows are four lines tall stops
                      being a list you can scan. */}
                  <span className="flex items-center gap-1 whitespace-nowrap">
                    {runner.labels.slice(0, 3).map((label) => (
                      <LabelTag key={label}>{label}</LabelTag>
                    ))}
                    {runner.labels.length > 3 && (
                      <span className="text-xs text-muted-foreground">
                        +{runner.labels.length - 3}
                      </span>
                    )}
                  </span>
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">
                  {runner.retired_at
                    ? `retired ${relativeTime(runner.retired_at, now)}`
                    : runner.last_heartbeat_age_seconds === null
                      ? 'never'
                      : `${duration(runner.last_heartbeat_age_seconds)} ago`}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </Panel>
  )
}
