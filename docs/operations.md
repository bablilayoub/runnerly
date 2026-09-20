# Running Runnerly in production

What to set up beyond getting it working, and what Runnerly does not do for
you.

## TLS

Machine tokens and session cookies are bearer credentials. Over plain HTTP,
anyone on the path can read them and use them. The server binds to
`127.0.0.1` by default so a fresh install is not exposed before you have
decided how to handle this.

Either terminate TLS in a reverse proxy — the usual arrangement — and set
`server.url` to the `https://` address, or let the server do it:

```yaml
server:
  listen: "0.0.0.0:8443"
  url: "https://runnerly.example.com"
  tls:
    cert_file: /etc/runnerly/tls/fullchain.pem
    key_file: /etc/runnerly/tls/privkey.pem
```

TLS 1.2 is the floor when the server serves it directly, since there is no
proxy to set a policy.

The server warns at startup if `server.url` is plain HTTP and not loopback,
and session cookies only get the `Secure` flag when the URL is `https://` —
setting it on a plain-HTTP server would stop sign-in working with no visible
reason.

## Rate limiting

```yaml
server:
  rate_limit:
    requests_per_minute: 600
    auth_requests_per_minute: 20
    trust_forwarded_for: false
```

Two limiters, both per client address. The strict one covers enrollment and
sign-in, because those are where guessing a token is worth an attacker's
time and each attempt costs them nothing and you a database round trip. The
general one covers everything, including those endpoints.

A refused request gets 429 and a `Retry-After`.

**`trust_forwarded_for` must stay off unless a proxy you control sets that
header.** Anyone can send `X-Forwarded-For`, so believing it lets a client
choose its own bucket and defeats the limit entirely.

The limiter is in memory. Behind several server instances each enforces its
own share, which is weaker than a shared limit — a real limitation, and the
alternative is Redis, which the project does not have a strong enough reason
for yet.

## Metrics

```yaml
server:
  metrics:
    enabled: true
    token: ""    # empty: open, which is normal on a private network
```

`GET /metrics` in Prometheus' text format:

```text
runnerly_runners_total           runners in service, retired ones excluded
runnerly_runners_online          online or starting
runnerly_runners_busy            running a job
runnerly_runners_offline         stopped reporting
runnerly_runners_retired         finished for good
runnerly_runner_heartbeats_total
runnerly_runner_restarts_total   restarts agents reported
runnerly_agent_errors_total      error-severity events from agents
runnerly_http_requests_total     by status class
runnerly_rate_limited_total
```

The gauges are recomputed every 15 seconds rather than queried per scrape, so
scraping cannot put load on the database or block on it. They are therefore
up to 15 seconds stale.

There is no Prometheus client library here. A handful of counters and gauges
is a few lines of text format, and the library would be the largest
dependency in the project. What that gives up: no histograms, so there are no
request-duration percentiles. Request durations are in the structured logs.

## Backups

Everything that cannot be recreated is in PostgreSQL. Back that up:

```bash
pg_dump --format=custom --file=runnerly-$(date -u +%Y%m%d).dump \
  "$RUNNERLY_DATABASE_URL"
```

Restore into an empty database:

```bash
pg_restore --dbname="$RUNNERLY_DATABASE_URL" --clean --if-exists runnerly.dump
```

The server applies migrations on start, so restoring an older dump and
starting a newer server brings the schema forward. Migrations are
forward-only: restoring a *newer* dump under an *older* server will not work.

**Back up the secret key separately**, wherever you keep secrets. It is not
in the database, and without it the user credentials sealed with it cannot be
read — a restore with a lost key leaves everyone needing to sign in again.
Nothing else is lost.

What is *not* worth backing up:

- **Runner directories.** Reinstall with `runnerly runner create`.
- **`credentials.yaml` on a runner machine.** Enroll again.
- **Ephemeral logs.** Copy them off if they matter; they are pruned anyway.

To check a backup is real, restore it into a scratch database and start a
server against it with `-migrate-only`. An untested backup is a guess.

## Credentials

| Credential | Stored as | Rotation |
| --- | --- | --- |
| Enrollment token | SHA-256 hash | single-use and expiring by default |
| Machine token | SHA-256 hash | automatic, every 24 hours |
| Session cookie | SHA-256 hash | expires after 7 days |
| A user's GitHub token | AES-256-GCM | when they sign in again |

Bearer credentials are hashed, not encrypted: the server only needs to
recognize one, and a hash cannot be turned back into a working credential by
someone with a database dump. A user's GitHub token is encrypted instead,
because the server may have to present it to GitHub.

Machine tokens rotate on their own. The server hands a replacement to the
agent on a heartbeat once the current one is a day old; the agent stores it
and uses it immediately, and the old one stops working at once. That bounds
how long a leaked token is worth anything — it is not a substitute for
revoking one you know has leaked:

```bash
runnerly runner remove <name>     # removes the runner and its credentials
```

## Audit trail

Removing a runner, asking for a restart, issuing or revoking an enrollment
token, and signing in are all recorded with who did it. Read them in the
dashboard under Settings, or:

```bash
curl -s --cookie "runnerly_session=$SESSION" https://runnerly.example.com/api/v1/audit
```

The trail is not pruned. It is small — one row per operator action, not per
request — and quietly discarding audit history is not a default anyone
should get by accident.

## Upgrades

```bash
runnerly upgrade              # report what is out of date
runnerly upgrade --runners    # bring this machine's runners up to date
```

Nothing upgrades on its own. An upgrade restarts a runner, and restarting one
mid-job throws that job away, so `--runners` refuses to touch a runner the
job hooks say is busy and asks before touching the rest.

`updates.auto` exists in the configuration and is **not acted on**. Unattended
upgrades need a maintenance window to be safe, and shipping the setting
without the window would be worse than not shipping it.

Runnerly does not replace its own binary. There are no published releases to
test that path against, and an untested binary-replacement routine is a bad
thing to find out about during an upgrade. `runnerly upgrade` tells you a
newer version exists; install it the way you installed the current one.

## Health

```bash
curl -fsS https://runnerly.example.com/api/v1/health
```

200 when the database answers, 503 with `"database":"unreachable"` when the
process is up but cannot do its job — so a load balancer takes it out of
rotation instead of sending it traffic.

## Logs

Everything is structured JSON on stdout: `time`, `level`, `component`,
`event`, plus fields. Filter on `event` for a transition, `level` for
anything wrong. Under systemd they land in the journal.

The control plane's event feed is not a log platform; it is pruned by
`server.event_retention_days`.

## What is not here

- **Multi-tenancy.** Everyone who can sign in sees every runner. The OAuth
  allow list is the only access control.
- **A shared rate limit** across server instances.
- **Request duration metrics.** In the logs, not in Prometheus.
- **Automatic upgrades**, for the reason above.
- **VM-per-job isolation.** Still the honest answer to "I need a real
  boundary"; see [security.md](security.md).
