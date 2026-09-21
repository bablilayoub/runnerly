import { useState } from 'react'
import { KeyRoundIcon, PlusIcon } from 'lucide-react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/ConfirmDialog'
import { CopyButton } from '@/components/CopyButton'
import { Panel } from '@/components/Panel'
import { Failure, Loading, Nothing } from '@/components/states'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/api'
import { orDash, relativeTime } from '@/format'
import { useLoad, useNow } from '@/hooks'
import type { EnrollmentToken, User } from '@/types'

export function Settings({ user }: { user: User }) {
  const tokens = useLoad(() => api.enrollmentTokens())
  const audit = useLoad(() => api.audit())
  const now = useNow()

  const [secret, setSecret] = useState<string>()
  const [revoking, setRevoking] = useState<EnrollmentToken | null>(null)

  async function create() {
    try {
      const result = await api.createEnrollmentToken({
        description: 'created from the dashboard',
        max_uses: 1,
        expires_in_hours: 24,
      })
      setSecret(result.secret)
      tokens.reload()
      // Issuing a token is an audited action, and the panel showing the
      // audit trail is on this same page. Leaving it stale made the page
      // say nothing had been recorded directly underneath the thing that
      // had just been recorded.
      audit.reload()
    } catch (err) {
      toast.error('Could not create an enrollment token', { description: message(err) })
    }
  }

  async function revoke(token: EnrollmentToken) {
    setRevoking(null)
    try {
      await api.revokeEnrollmentToken(token.id)
      toast.success('Enrollment token revoked')
      tokens.reload()
      audit.reload()
    } catch (err) {
      toast.error('Could not revoke the token', { description: message(err) })
    }
  }

  const commands = `export RUNNERLY_SERVER_URL=${window.location.origin}
export RUNNERLY_ENROLLMENT_TOKEN=${secret ?? ''}
runnerly agent run`

  return (
    <div className="space-y-6">
      <Panel title="Signed in as" bodyClassName="divide-y divide-border">
        <Field name="GitHub account" value={user.login} mono />
        <Field name="Last sign-in" value={relativeTime(user.last_login_at, now)} />
      </Panel>

      {secret && (
        <Panel
          title="New enrollment token"
          action={
            <Button variant="ghost" size="sm" onClick={() => setSecret(undefined)}>
              Done
            </Button>
          }
        >
          <div className="space-y-3 px-4 py-4">
            <p className="text-sm text-muted-foreground">
              This is the only time it is shown. The server keeps only its hash.
            </p>
            <div className="flex items-center gap-1 rounded-lg border border-border bg-muted/40 py-1.5 pr-1.5 pl-3">
              <code className="min-w-0 flex-1 overflow-x-auto font-mono text-xs whitespace-nowrap">
                {secret}
              </code>
              <CopyButton value={secret} label="Copy the token" />
            </div>
            <p className="text-sm text-muted-foreground">On the runner machine:</p>
            <div className="flex items-start gap-1 rounded-lg border border-border bg-muted/40 py-2 pr-1.5 pl-3">
              <pre className="min-w-0 flex-1 overflow-x-auto font-mono text-xs">{commands}</pre>
              <CopyButton value={commands} label="Copy the commands" />
            </div>
          </div>
        </Panel>
      )}

      <Panel
        title="Enrollment tokens"
        action={
          <Button variant="outline" size="sm" onClick={() => void create()}>
            <PlusIcon />
            Create token
          </Button>
        }
      >
        {tokens.initial ? (
          <Loading rows={3} />
        ) : tokens.error ? (
          <div className="p-4">
            <Failure error={tokens.error} />
          </div>
        ) : (tokens.data?.tokens.length ?? 0) === 0 ? (
          <Nothing
            title="No enrollment tokens"
            hint="A machine needs one to enroll. Tokens created here allow a single machine and expire after a day."
          >
            <Button variant="outline" size="sm" onClick={() => void create()}>
              <KeyRoundIcon />
              Create one
            </Button>
          </Nothing>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Created</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Uses</TableHead>
                <TableHead>State</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.data!.tokens.map((token) => {
                const state = tokenState(token, now)
                return (
                  <TableRow key={token.id}>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {relativeTime(token.created_at, now)}
                    </TableCell>
                    <TableCell>{orDash(token.description)}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {token.uses}
                      {token.max_uses ? `/${token.max_uses}` : ''}
                    </TableCell>
                    <TableCell className={state === 'usable' ? '' : 'text-muted-foreground'}>
                      {state}
                    </TableCell>
                    <TableCell className="text-right">
                      {state === 'usable' && (
                        <Button variant="ghost" size="xs" onClick={() => setRevoking(token)}>
                          Revoke
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </Panel>

      <Panel title="Audit trail">
        {audit.initial ? (
          <Loading rows={3} />
        ) : audit.error ? (
          <div className="p-4">
            <Failure error={audit.error} />
          </div>
        ) : (audit.data?.entries.length ?? 0) === 0 ? (
          <Nothing
            title="Nothing recorded yet"
            hint="Removing a runner, asking for a restart and issuing a token are all recorded here."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Who</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Target</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {audit.data!.entries.map((entry) => (
                <TableRow key={entry.id}>
                  <TableCell className="whitespace-nowrap text-muted-foreground">
                    {relativeTime(entry.created_at, now)}
                  </TableCell>
                  <TableCell>{entry.actor}</TableCell>
                  <TableCell className="font-mono text-xs">{entry.action}</TableCell>
                  <TableCell className="text-muted-foreground">{orDash(entry.target)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>

      {revoking && (
        <ConfirmDialog
          title="Revoke this enrollment token?"
          danger
          body={
            <p>
              Any machine that has not used it yet will no longer be able to enroll. Machines that
              already enrolled are unaffected: they hold their own credential.
            </p>
          }
          confirmLabel="Revoke"
          onConfirm={() => void revoke(revoking)}
          onCancel={() => setRevoking(null)}
        />
      )}
    </div>
  )
}

function Field({ name, value, mono }: { name: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4 px-4 py-2 text-sm">
      <dt className="text-muted-foreground">{name}</dt>
      <dd className={mono ? 'font-mono text-xs' : ''}>{value}</dd>
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

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}
