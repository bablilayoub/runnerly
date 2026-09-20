import { useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useLoad, useNow } from '../hooks'
import { duration, orDash } from '../format'
import { Card, Empty, Failure, Label, Spinner, Status } from '../components/primitives'
import { Cell, Row, Table } from '../components/Table'

const POLL_MS = 5000

export function Runners() {
  const { data, error, initial } = useLoad(() => api.runners(), POLL_MS)
  const navigate = useNavigate()
  useNow() // keep the heartbeat ages moving between polls

  if (initial) return <Spinner />
  if (error) return <Failure error={error} />

  const runners = data?.runners ?? []

  return (
    <Card title={`Runners (${runners.length})`}>
      {runners.length === 0 ? (
        <Empty
          title="No runners have enrolled"
          hint="This lists what the control plane knows about, which is not the same as what GitHub has registered."
        />
      ) : (
        <Table head={['Name', 'Status', 'Scope', 'Platform', 'Labels', 'Heartbeat']}>
          {runners.map((runner) => (
            <Row key={runner.id} onClick={() => navigate(`/runners/${runner.id}`)}>
              <Cell>
                <span className="font-medium whitespace-nowrap">{runner.name}</span>
                {runner.ephemeral && (
                  <span className="ml-2 text-xs" style={{ color: 'var(--text-faint)' }}>
                    ephemeral
                  </span>
                )}
              </Cell>
              <Cell>
                <Status status={runner.status} health={runner.health} />
              </Cell>
              <Cell mono muted>
                {runner.github_scope_id}
              </Cell>
              <Cell muted>
                {orDash(runner.os)}/{orDash(runner.architecture)}
              </Cell>
              <Cell>
                <span className="flex flex-wrap gap-1">
                  {runner.labels.slice(0, 4).map((label) => (
                    <Label key={label}>{label}</Label>
                  ))}
                  {runner.labels.length > 4 && (
                    <span className="text-xs" style={{ color: 'var(--text-faint)' }}>
                      +{runner.labels.length - 4}
                    </span>
                  )}
                </span>
              </Cell>
              <Cell muted>
                {runner.last_heartbeat_age_seconds === null
                  ? 'never'
                  : `${duration(runner.last_heartbeat_age_seconds)} ago`}
              </Cell>
            </Row>
          ))}
        </Table>
      )}
    </Card>
  )
}
