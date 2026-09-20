# The control plane

`runnerly-server` keeps track of the machines that run your jobs: which exist,
whether they are alive, and what has happened to them.

It does **not** schedule anything. GitHub still assigns jobs to runners. The
control plane manages the machines around them.

```text
agents  ──heartbeats, events──▶  runnerly-server  ──▶  PostgreSQL
                                        │
                                        └──▶  API (dashboard, later)
```

It is optional. The CLI and the agent both work without one; a single machine
never needs it. It earns its place when you have several and want one answer
to "what is out there and is it healthy?".

## Running it

```bash
cp deploy/compose/.env.example deploy/compose/.env
# fill in POSTGRES_PASSWORD, then:
docker compose -f deploy/compose/docker-compose.yml up -d
```

Or from a binary, with PostgreSQL somewhere:

```bash
export RUNNERLY_DATABASE_URL='postgres://runnerly:secret@localhost:5432/runnerly?sslmode=disable'
runnerly-server
```

Migrations run on start. To run them separately:

```bash
runnerly server migrate        # or: runnerly-server -migrate-only
```

They are forward-only. Each runs in its own transaction with the row recording
it, so a failure leaves the database on the last version that fully applied.
There is no automatic rollback: reversing a schema in production is a decision
to make deliberately, not a button to press.

### It binds to loopback

`server.listen` defaults to `127.0.0.1:8080`, so a fresh install is not
exposed before you have thought about TLS. Machine tokens are bearer
credentials — plain HTTP hands them to anyone on the path. Put a reverse proxy
with TLS in front before changing this.

## Enrolling a machine

An agent needs a credential. Issue one on the server:

```bash
runnerly server enrollment-token create --description "build box" --max-uses 1
```

The secret prints once. The server stores only its hash, so a database dump
cannot be turned back into a working credential.

On the runner machine:

```bash
export RUNNERLY_SERVER_URL=https://runnerly.example.com
export RUNNERLY_ENROLLMENT_TOKEN=rnr_enroll_...
runnerly agent run
```

The agent trades the enrollment token for a machine token of its own, stores
that in `credentials.yaml` (mode 0600) and never needs the enrollment token
again. Set `RUNNERLY_MACHINE_TOKEN` instead if you would rather it never
touched disk.

Enrollment is idempotent: the same host, scope and name always resolve to the
same runner, so a rebuilt machine does not leave a duplicate behind.

```bash
runnerly server enrollment-token list
runnerly server enrollment-token revoke <id>
```

## Dashboard sign-in

Runnerly ships no OAuth credentials: a GitHub OAuth App belongs to whoever
runs the server. Until you configure one, sign-in is disabled and the server
says exactly which settings are missing. Agent endpoints work regardless.

Register an OAuth App with callback
`https://<your-host>/api/v1/auth/github/callback`, then:

```yaml
server:
  url: https://runnerly.example.com
  oauth:
    client_id: Iv1.xxxxxxxx
    allowed_logins: [octocat]
```

with `RUNNERLY_OAUTH_CLIENT_SECRET` in the environment.

**Set `allowed_logins`.** Left empty, any GitHub account that completes the
flow gets in. The server warns about this at startup, and it is only
acceptable on a server that is not reachable from the internet.

Runnerly asks GitHub only for `read:user read:org`. The dashboard reads
Runnerly's own database, so it needs no repository access.

## The secret key

```bash
runnerly server keygen
```

Set it as `RUNNERLY_SECRET_KEY`. It encrypts signed-in users' GitHub tokens
with AES-256-GCM.

Running without one is a legitimate choice: the server then does not store
user tokens at all, and warns so it is not a surprise. Losing a key makes
anything sealed with it unreadable.

Bearer tokens — enrollment, machine and session — are **hashed**, not
encrypted, because the server only ever needs to recognize one, never read it
back.

## API

Everything is under `/api/v1`. Errors share one shape: `error`, `message` and
a `hint` saying what to do.

| Endpoint | Auth | Purpose |
| --- | --- | --- |
| `GET /health` | none | liveness, plus whether the database answers |
| `POST /agent/register` | enrollment token | enroll, receive a machine token |
| `POST /agent/heartbeat` | machine token | report status and metrics |
| `POST /agent/events` | machine token | report a batch of events |
| `GET /agent/config` | machine token | what the server expects of this agent |
| `GET /auth/github` | none | start sign-in |
| `GET /auth/github/callback` | none | finish sign-in |
| `GET /auth/session` | session | who is signed in |
| `POST /auth/logout` | session | end the session |
| `GET /overview` | session | fleet counts and recent events |
| `GET /runners` | session | list runners |
| `GET /runners/{id}` | session | one runner |
| `DELETE /runners/{id}` | session | forget a runner |
| `POST /runners/{id}/restart` | session | queue a restart for the next heartbeat |
| `GET /runners/{id}/commands` | session | that runner's recent commands |
| `POST /agent/commands/{id}/result` | machine token | report a command's outcome |
| `GET /auth/config` | none | whether sign-in is available |
| `GET /audit` | session | the audit trail |
| `GET /metrics` | optional token | Prometheus metrics |
| `GET /events` | session | the event feed |
| `GET /enrollment-tokens` | session | list tokens |
| `POST /enrollment-tokens` | session | issue a token |
| `DELETE /enrollment-tokens/{id}` | session | revoke a token |

Unknown JSON fields are rejected rather than ignored, so a misspelled field
fails loudly instead of silently doing nothing.

`DELETE /runners/{id}` removes Runnerly's record only. It does not deregister
the runner with GitHub and does not touch the machine; the response says so.
Use `runnerly runner remove` for that.

## Health and status

A runner's status is not simply what it last said. An agent that dies cannot
send "offline", so the server compares the last heartbeat against its
thresholds and reports what follows:

```yaml
server:
  heartbeat:
    interval: 20s        # how often agents report
    stale_after: 30s     # slower than this and it is stale
    offline_after: 90s   # nothing since then and it is offline
```

`interval` must be shorter than `stale_after`, or every runner looks stale
between heartbeats. The configuration is validated for this rather than
letting you find out from a dashboard full of warnings.

`GET /health` returns 503 when the database is unreachable, so a load balancer
takes the instance out of rotation instead of sending it traffic it cannot
serve.

## Retention

The control plane is not a log platform. The event feed is pruned hourly:

```yaml
server:
  event_retention_days: 30
```

Expired sessions are pruned on the same schedule.

## Hardening

TLS, rate limiting, metrics, credential rotation, backups and the audit
trail are covered in [operations.md](operations.md).

## What is not implemented

- **Automatic upgrades.** `runnerly upgrade` reports and applies them on
  request; doing it unattended needs a maintenance window to be safe.
- **Multi-tenancy.** Every signed-in user sees every runner. The allow list is
  the only access control.
