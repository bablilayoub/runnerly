# Configuration

Runnerly reads a single YAML file. Every field has a default, and a missing
file is not an error — `runnerly doctor` works on a machine that has never
been configured.

## Where the file lives

Resolution order, first match wins:

1. `--config /path/to/config.yaml`
2. `$RUNNERLY_CONFIG`
3. `$XDG_CONFIG_HOME/runnerly/config.yaml`, or `~/.config/runnerly/config.yaml`
4. `/etc/runnerly/config.yaml`, if it exists and the per-user file does not

```bash
runnerly config path
```

The file is written with mode `0600`. It holds no secret by default — those
belong in the environment — but `server.secret_key` and
`server.oauth.client_secret` can be set here, so the mode has to assume they
are.

`runnerly config init` writes the whole file with every default already in it
and a comment on each field. This page is the same reference, with the
reasoning.

## Commands

```bash
runnerly config init        # write a default file (--force to overwrite)
runnerly config show        # effective settings, defaults included
runnerly config validate    # check the file before relying on it
```

`config show` prints the merged result, so it is the authoritative answer to
"what is Runnerly actually using?".

Unknown keys are a hard error. A typo like `executer:` fails loudly at load
time rather than being silently ignored.

## Reference

Every field is shown at its default.

### server

Only `runnerly-server` reads most of these; the CLI and agent read `url`.

```yaml
server:
  # Runnerly control plane. Optional: the CLI and agent work without one.
  url: ""
  # Address runnerly-server binds. Loopback by default, so a fresh install
  # is not exposed before TLS is in front of it.
  listen: "127.0.0.1:8080"
  # PostgreSQL connection string. Prefer RUNNERLY_DATABASE_URL.
  database: ""
  # 32 bytes, hex or base64, encrypting user credentials at rest.
  # Prefer RUNNERLY_SECRET_KEY. Generate one: runnerly server keygen
  secret_key: ""
  oauth:
    # A GitHub OAuth App belongs to whoever runs the server, so Runnerly
    # ships none. Until these are set, dashboard sign-in is off.
    client_id: ""
    client_secret: ""
    # Who may sign in. Empty allows any GitHub account that completes the
    # flow, which is only safe on a server the internet cannot reach.
    allowed_logins: []
  heartbeat:
    # How often an agent reports, and when the server stops believing it.
    interval: "20s"
    stale_after: "30s"
    offline_after: "1m30s"
  # How long the event feed keeps history.
  event_retention_days: 30
  # How long an agent uses a machine token before the server replaces it.
  token_lifetime: "24h0m0s"
  tls:
    # Serve HTTPS directly. Both empty terminates TLS in a reverse proxy,
    # which is the more common deployment.
    cert_file: ""
    key_file: ""
  rate_limit:
    # Per client address, per minute. 0 disables limiting.
    requests_per_minute: 600
    # Enrollment and sign-in, where guessing is worth an attacker's time.
    auth_requests_per_minute: 20
    # Read the client address from X-Forwarded-For. Only behind a proxy you
    # control: anyone can send that header.
    trust_forwarded_for: false
  metrics:
    enabled: true
    # Require this as a bearer token to scrape. Empty leaves it open.
    token: ""
```

`interval` must be shorter than `stale_after`, and validation says so rather
than letting a fleet report itself permanently stale. See
[server.md](server.md) and [operations.md](operations.md).

### agent

```yaml
agent:
  # One-shot token used to enroll with the control plane. Prefer
  # RUNNERLY_ENROLLMENT_TOKEN. It is consumed once; the machine token that
  # replaces it is stored in credentials.yaml.
  enrollment_token: ""
```

### github

```yaml
github:
  # GitHub hostname. Use github.com unless you run GitHub Enterprise Server.
  host: github.com
  # "repository" or "organization".
  scope: repository
  # owner/repo, when scope is repository.
  repository: ""
  # Organization login, required when scope is organization.
  organization: ""
```

### runner

```yaml
runner:
  # Runner name as it appears in GitHub. Letters, digits, '.', '_', '-'.
  # Empty means Runnerly generates one.
  name: ""
  # GitHub runner labels. These map directly to GitHub's; Runnerly adds no
  # routing system of its own. The platform labels (self-hosted, the OS,
  # the architecture) are always added on top, so naming them here is
  # unnecessary and is how they get to disagree.
  labels:
    - runnerly
  # Where the official GitHub runner is installed. Empty uses
  # /opt/runnerly/runners for root, or ~/.local/share/runnerly/runners.
  dir: ""
```

### executor

```yaml
executor:
  # "host"   - the GitHub runner executes jobs directly on the machine
  # "docker" - the machine runs the runner with Docker available for
  #            containerized steps and job containers
  type: docker
  docker:
    # Overrides DOCKER_HOST for the runner. Empty uses Docker's default.
    host: ""
    # What to do with what a job leaves behind: after_job or never. Only
    # what appeared during the job is removed. See docs/docker.md.
    cleanup: "after_job"
    # Also remove dangling images. Off by default: they are cache.
    prune_images: false
    # Bounds each docker command the agent runs.
    timeout: "2m0s"
```

### ephemeral

```yaml
ephemeral:
  # Where a destroyed runner's output is kept. Empty uses a logs directory
  # beside this file — outside the runner's own directory on purpose, so
  # the logs outlive the runner.
  log_dir: ""
  # How many runs of logs to keep. 0 keeps every one.
  keep_runs: 50
  # The start of each generated runner name. Empty uses the hostname.
  name_prefix: ""
```

See [ephemeral-runners.md](ephemeral-runners.md).

### updates and security

```yaml
updates:
  # Whether Runnerly may upgrade itself and the GitHub runner unattended.
  auto: false

security:
  # See docs/security.md.
  allow_public_repositories: false
  allow_fork_workflows: false
```

### Defaults worth knowing

`runner.labels` defaults to a single `runnerly` marker, so Runnerly-managed
runners are identifiable in the GitHub UI. The platform labels are added at
registration from the platform Runnerly detects, so a Linux x86_64 machine
registers `self-hosted,linux,x64,runnerly` and a Mac registers
`self-hosted,macOS,arm64,runnerly`.

The default deliberately does not name the OS or the architecture. It used
to, using Go's names — which meant a Mac registered both `darwin` and
GitHub's own `macOS`. One source for a fact is better than two that can
disagree.

`executor.type` defaults to `docker`. If Docker is not installed and you do not
want it, set `type: host` and `doctor` stops requiring Docker.

## Effect on doctor

The configuration changes what `doctor` checks:

| Setting | Effect |
| --- | --- |
| `executor.type: host` | the Docker checks are skipped |
| `executor.type: docker` | a missing Docker CLI is a failure, and disk space is checked |
| `runner.dir` | where `runner create` installs, plus the runner name |
| `server.url: ""` | the control-plane check is skipped |
| `server.url` set | `GET <url>/api/v1/health` must return 200 |
| `github.host` | chooses the API endpoint that must be reachable |
| `github.scope` | chooses which token scope the credentials check expects |

## Credentials

The GitHub token is **not** in this file. It lives in `credentials.yaml` beside
it, written with mode 0600, and is managed with `runnerly login` and
`runnerly logout`. See [github.md](github.md) and [security.md](security.md).

## Installed runners

Neither is the record of what is installed. `runners.yaml` sits beside this
file and lists the runners on this machine: their names, scopes and
directories. `runner create` writes it and `runner remove` prunes it, so
nothing here is ever rewritten under you.

It is why `runner list`, `runner remove` and the agent work without `--repo` on
a machine with one runner. See [agent.md](agent.md).

## Control plane settings

`server.*` and `agent.enrollment_token` configure the optional control plane
and the agent's connection to it. They are documented in
[server.md](server.md); the short version is that secrets belong in the
environment rather than this file:

| Setting | Environment variable |
| --- | --- |
| `server.database` | `RUNNERLY_DATABASE_URL` |
| `server.secret_key` | `RUNNERLY_SECRET_KEY` |
| `server.oauth.client_secret` | `RUNNERLY_OAUTH_CLIENT_SECRET` |
| `server.url` | `RUNNERLY_SERVER_URL` |
| `agent.enrollment_token` | `RUNNERLY_ENROLLMENT_TOKEN` |

## Settings that are recorded but not acted on

Two fields describe an intent Runnerly cannot currently carry out. They are
listed here rather than left for you to discover.

`updates.auto` is read and validated, and nothing acts on it. `runnerly
upgrade` reports what is out of date and, with `--runners`, upgrades this
machine's runners — but only when you ask. Upgrading a runner restarts it,
and restarting one mid-job throws that job away, so doing it unattended needs
a maintenance window that does not exist yet.

`security.allow_fork_workflows` is recorded and not enforced: Runnerly cannot
control which workflows GitHub dispatches. Enforce it in GitHub's settings.
`security.allow_public_repositories` **is** enforced, by `runner create`.
