# Runnerly

Run GitHub Actions on your own infrastructure.

Runnerly manages self-hosted GitHub Actions runners: installing them,
registering them with GitHub, keeping them running, cleaning up after them,
and showing you what they are doing.

It is **not** a replacement for GitHub Actions. GitHub still orchestrates
workflows and assigns jobs. Runnerly manages the machines around
[GitHub's official runner](https://github.com/actions/runner), which it
downloads and drives rather than reimplements.

```text
        GitHub  ──  schedules and assigns jobs
          │
          ▼
      Runnerly  ──  installs, registers, monitors, recovers, retires runners
          │
    ┌─────┼─────┐
    ▼     ▼     ▼
  runner runner runner
```

## Quick start

Requires Go 1.25 or newer. There is no installer yet; build from source:

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make all           # dashboard, then binaries
sudo make install  # optional: /usr/local/bin
```

Check the machine, then register a runner:

```bash
runnerly doctor
echo "$GITHUB_TOKEN" | runnerly login --with-token
runnerly runner create --repo owner/repo
runnerly agent run
```

```text
✓ runnerly-01 is registered with owner/repo

  scope    owner/repo
  labels   self-hosted,linux,x64
  runner   actions-runner-linux-x64-2.330.0.tar.gz
  dir      /home/me/.local/share/runnerly/runners/runnerly-01
```

The archive is verified against the SHA-256 checksum GitHub publishes with it
before anything is unpacked or executed.

Workflows reach the runner the usual way — the labels are GitHub's own, and
`--labels` adds your own on top:

```yaml
jobs:
  test:
    runs-on: [self-hosted, linux, x64]
```

## What it does

**Sets a machine up.** `runnerly doctor` checks everything that would stop a
runner working — Docker, disk, network, credentials, scopes — and every
failure names a command to fix it.

```text
✗ docker daemon
  Docker is installed but the daemon did not respond.
  Cannot connect to the Docker daemon at unix:///var/run/docker.sock.
  Try:
    sudo systemctl start docker
```

It exits non-zero, so it works as a gate in a provisioning script. `--json`
for machine-readable output, `--offline` to skip network checks.

**Keeps the runner running.** The agent restarts a failed runner on a 5s,
10s, 20s, 40s, 80s backoff, then stops rather than hiding one that cannot
start. systemd restarts the agent — two layers, each covering the other's
failure.

```bash
runnerly agent run        # foreground
runnerly agent systemd    # print a unit; installs nothing itself
```

**Cleans up after jobs.** With the Docker executor, Runnerly removes the
containers, volumes and networks a job created — and only those. It records
what existed when the job started rather than running `docker system prune`,
which would take a container belonging to something else on the machine.

**Runs one-job runners.** `runnerly ephemeral run` does the whole lifecycle:
register, take one job, clean up, deregister, destroy. Its logs are kept
outside the runner directory, so they survive the runner being taken apart.
Pair it with a `Restart=always` unit and the machine always has one fresh
runner.

**Tracks a fleet.** An optional control plane stores what exists, what is
healthy, and what happened, with a dashboard on top. A single machine never
needs one.

## Several machines

```bash
docker compose -f deploy/compose/docker-compose.yml up -d
runnerly server enrollment-token create --max-uses 1
```

On each runner machine:

```bash
export RUNNERLY_SERVER_URL=https://runnerly.example.com
export RUNNERLY_ENROLLMENT_TOKEN=rnr_enroll_...
runnerly agent run
```

The agent trades that one-shot token for a credential of its own, which the
server rotates daily. The dashboard is served by the same binary at `/`:

```text
Runners
  3 total   1 online   1 busy   1 offline

runnerly-01   ● online   bablilayoub/runnerly   linux/x64    28s ago
runnerly-02   ● busy     bablilayoub/runnerly   linux/x64    28s ago
build-arm-01  ○ offline  acme/widgets           linux/arm64  never
```

`busy` is what the runner itself reported through GitHub's job hooks, not a
guess from the process being alive.

## Security

Self-hosted runners execute code from your workflows. Read
[docs/security.md](docs/security.md) before pointing one at a public
repository or at pull requests from forks.

What Runnerly does about it:

- refuses to register against a public repository unless you explicitly allow
  it
- verifies every download against GitHub's published checksum, and deletes a
  file that fails rather than executing it
- rejects archive entries that would be written outside the install directory
- hashes every bearer credential, and encrypts user tokens at rest
- rotates machine tokens, rate-limits enrollment and sign-in
- never takes privileges it was not handed: `systemd` subcommands print units
  and the commands to install them instead of running `sudo` for you

What it does not do: **Docker is not a security boundary.** Anything that can
reach the Docker socket has root on the host. See
[docs/docker.md](docs/docker.md).

## Platforms

| | |
| --- | --- |
| Linux, x86-64 and arm64 | developed and tested here |
| macOS | runners work; no service integration, since the generated unit is systemd |
| Windows | not supported — the job hooks are shell scripts |

GitHub.com is supported. GitHub Enterprise Server is implemented but has not
been exercised against a real instance.

## Not built

Deliberately, for now:

- **Autoscaling and cloud provisioning.** Nothing decides how many machines to
  have or creates them.
- **VM-per-job isolation.** The honest answer when you need a real boundary.
- **Unattended upgrades.** `runnerly upgrade` reports and applies them on
  request; doing it automatically needs a maintenance window to be safe.
- **Self-replacing binary.** There are no published releases to test that path
  against.
- **`runnerly setup`**, the single interactive command. Its pieces all exist;
  the wrapper does not.
- **An installer.** No `install.sh`, no Homebrew tap.

## Documentation

Start with [getting started](docs/getting-started.md).

| | |
| --- | --- |
| [Installation](docs/installation.md) | building and installing |
| [GitHub integration](docs/github.md) | tokens, scopes, discovery |
| [Runners](docs/runners.md) | creating, listing, removing |
| [The agent](docs/agent.md) | supervision, restarts, systemd |
| [The Docker executor](docs/docker.md) | job cleanup and its boundaries |
| [Ephemeral runners](docs/ephemeral-runners.md) | one-job lifecycle |
| [The control plane](docs/server.md) | the server and its API |
| [The dashboard](docs/dashboard.md) | the web interface |
| [Running in production](docs/operations.md) | TLS, metrics, backups, upgrades |
| [Configuration](docs/configuration.md) | every setting |
| [Security model](docs/security.md) | what to know before you deploy |
| [Troubleshooting](docs/troubleshooting.md) | when something is wrong |
| [Architecture](docs/architecture.md) | how it fits together, and why |
| [Development](docs/development.md) | working on Runnerly |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues and pull requests are welcome.

## License

MIT. See [LICENSE](LICENSE).
