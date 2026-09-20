import { useState } from 'react'
import { api } from '../api'
import { useLoad, useNow } from '../hooks'
import { orDash, relativeTime } from '../format'
import { Button, Card, Empty, Failure, Spinner } from '../components/primitives'
import { Cell, Row, Table } from '../components/Table'
import { Confirm } from '../components/Confirm'
import type { EnrollmentToken, User } from '../types'

export function Settings({ user }: { user: User }) {
  const tokens = useLoad(() => api.enrollmentTokens())
  const audit = useLoad(() => api.audit())
  const now = useNow()

  const [secret, setSecret] = useState<string>()
  const [actionError, setActionError] = useState<Error>()
  const [revoking, setRevoking] = useState<EnrollmentToken | null>(null)

  async function create() {
    setActionError(undefined)
    try {
      const result = await api.createEnrollmentToken({
        description: 'created from the dashboard',
        max_uses: 1,
        expires_in_hours: 24,
      })
      setSecret(result.secret)
      tokens.reload()
    } catch (err) {
      setActionError(err instanceof Error ? err : new Error(String(err)))
    }
  }

  async function revoke(token: EnrollmentToken) {
    setRevoking(null)
    setActionError(undefined)
    try {
      await api.revokeEnrollmentToken(token.id)
      tokens.reload()
    } catch (err) {
      setActionError(err instanceof Error ? err : new Error(String(err)))
    }
  }

  return (
    <div className="space-y-6">
      <Card title="Signed in as">
        <dl className="divide-y text-sm" style={{ borderColor: 'var(--border)' }}>
          <div className="flex justify-between px-4 py-2">
            <dt style={{ color: 'var(--text-muted)' }}>GitHub account</dt>
            <dd className="font-mono">{user.login}</dd>
          </div>
          <div className="flex justify-between px-4 py-2">
            <dt style={{ color: 'var(--text-muted)' }}>Last sign-in</dt>
            <dd>{relativeTime(user.last_login_at, now)}</dd>
          </div>
        </dl>
      </Card>

      {actionError && <Failure error={actionError} />}

      {secret && (
        <Card title="New enrollment token">
          <div className="space-y-3 px-4 py-3">
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              This is the only time it is shown. The server keeps only its hash.
            </p>
            <code
              className="block overflow-x-auto rounded-sm border px-3 py-2 font-mono text-xs"
              style={{ borderColor: 'var(--border-strong)', background: 'var(--bg-subtle)' }}
            >
              {secret}
            </code>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              On the runner machine:
            </p>
            <pre
              className="overflow-x-auto rounded-sm border px-3 py-2 font-mono text-xs"
              style={{ borderColor: 'var(--border)', background: 'var(--bg-subtle)' }}
            >
              {`export RUNNERLY_SERVER_URL=${window.location.origin}
export RUNNERLY_ENROLLMENT_TOKEN=${secret}
runnerly agent run`}
            </pre>
            <Button onClick={() => setSecret(undefined)}>Done</Button>
          </div>
        </Card>
      )}

      <Card title="Enrollment tokens">
        <div className="flex justify-end px-4 py-3">
          <Button onClick={() => void create()}>Create token</Button>
        </div>

        {tokens.initial ? (
          <Spinner />
        ) : tokens.error ? (
          <Failure error={tokens.error} />
        ) : (tokens.data?.tokens.length ?? 0) === 0 ? (
          <Empty
            title="No enrollment tokens"
            hint="A machine needs one to enroll. Tokens created here allow a single machine and expire after a day."
          />
        ) : (
          <Table head={['Created', 'Description', 'Uses', 'State', '']}>
            {tokens.data!.tokens.map((token) => {
              const state = tokenState(token, now)
              return (
                <Row key={token.id}>
                  <Cell muted>{relativeTime(token.created_at, now)}</Cell>
                  <Cell>{orDash(token.description)}</Cell>
                  <Cell muted>
                    {token.uses}
                    {token.max_uses ? `/${token.max_uses}` : ''}
                  </Cell>
                  <Cell muted={state !== 'usable'}>{state}</Cell>
                  <Cell>
                    {state === 'usable' && (
                      <button
                        onClick={() => setRevoking(token)}
                        className="cursor-pointer text-xs underline"
                        style={{ color: 'var(--text-muted)' }}
                      >
                        Revoke
                      </button>
                    )}
                  </Cell>
                </Row>
              )
            })}
          </Table>
        )}
      </Card>

      <Card title="Audit trail">
        {audit.initial ? (
          <Spinner />
        ) : audit.error ? (
          <Failure error={audit.error} />
        ) : (audit.data?.entries.length ?? 0) === 0 ? (
          <Empty
            title="Nothing recorded yet"
            hint="Removing a runner, asking for a restart and issuing a token are all recorded here."
          />
        ) : (
          <Table head={['When', 'Who', 'Action', 'Target']}>
            {audit.data!.entries.map((entry) => (
              <Row key={entry.id}>
                <Cell muted>{relativeTime(entry.created_at, now)}</Cell>
                <Cell>{entry.actor}</Cell>
                <Cell mono>{entry.action}</Cell>
                <Cell muted>{orDash(entry.target)}</Cell>
              </Row>
            ))}
          </Table>
        )}
      </Card>

      {revoking && (
        <Confirm
          title="Revoke this enrollment token?"
          danger
          body="Any machine that has not used it yet will no longer be able to enroll. Machines that already enrolled are unaffected: they hold their own credential."
          confirmLabel="Revoke"
          onConfirm={() => void revoke(revoking)}
          onCancel={() => setRevoking(null)}
        />
      )}
    </div>
  )
}

/** tokenState mirrors what the server considers usable. */
function tokenState(token: EnrollmentToken, now: number): string {
  if (token.revoked_at) return 'revoked'
  if (token.expires_at && Date.parse(token.expires_at) <= now) return 'expired'
  if (token.max_uses !== undefined && token.max_uses !== null && token.uses >= token.max_uses) {
    return 'used up'
  }
  return 'usable'
}
