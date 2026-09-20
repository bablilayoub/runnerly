# Runnerly

Run GitHub Actions on your own infrastructure.

Runnerly manages self-hosted GitHub Actions runners: installing them,
registering them with GitHub, keeping them alive, and showing you what they are
doing.

It is **not** a replacement for GitHub Actions. GitHub still orchestrates
workflows and assigns jobs. Runnerly manages the machines around
[GitHub's official runner](https://github.com/actions/runner).

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

## Status

Early development. The CLI can check a machine, authenticate with GitHub, and
install and register a real self-hosted runner. It does not yet keep that
runner running.

| Area | State |
| --- | --- |
| `runnerly doctor`, `config`, `version` | works |
| `runnerly login` / `logout` / `auth status` | works |
| `runnerly repo list` | works |
| `runnerly runner create` / `list` / `status` / `remove` | works |
| `runnerly agent run` / `status` / `systemd` | works |
| Automatic restart with backoff, systemd service | works |
| Upgrades and ephemeral lifecycle | not implemented |
| Control plane, heartbeats, dashboard | not implemented |

A runner installed here stays up: the agent restarts it when it fails, and
systemd restarts the agent. See [docs/architecture.md](docs/architecture.md)
for where this is going.

## Try it

Requires Go 1.24 or newer.

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make build
./dist/runnerly doctor
```

Then point it at a repository:

```bash
echo "$GITHUB_TOKEN" | runnerly login --with-token
runnerly repo list
runnerly runner create --repo owner/repo
runnerly agent run
```

```text
Registering runnerly-01

  resolving the runner release GitHub expects
  downloading actions-runner-linux-x64-2.x.x.tar.gz
  unpacking the runner
  registering with GitHub

✓ runnerly-01 is registered with owner/repo

  scope    owner/repo
  labels   self-hosted,linux,x64,docker
  dir      /home/me/.local/share/runnerly/runners/runnerly-01

Start it:
  runnerly agent run runnerly-01
```

The archive is checked against the SHA-256 checksum GitHub publishes with it
before anything is unpacked or executed.

`doctor` inspects the machine and explains anything that would stop a runner
from working:

```text
Runnerly Doctor

✓ configuration
✓ operating system
✓ architecture
✓ git
✓ curl
✓ docker
✗ docker daemon
  Docker is installed but the daemon did not respond.
  Cannot connect to the Docker daemon at unix:///var/run/docker.sock.
  Try:
    sudo systemctl start docker
○ Runnerly server
  no control plane configured; the CLI does not require one

✗ 1 check(s) failed. Fix the items above, then run runnerly doctor again.
```

It exits non-zero when a check fails, so it works in a provisioning script.
`--json` gives machine-readable output, and `--offline` skips network checks.

## Keeping it running

```bash
runnerly agent run              # foreground, restarts the runner if it fails
runnerly agent status           # what is installed on this machine
runnerly agent systemd          # print a service unit; installs nothing itself
```

The agent restarts a failed runner on a 5s, 10s, 20s, 40s, 80s backoff and then
stops rather than hiding a runner that cannot start. systemd restarts the
agent. Logs are structured JSON on stdout.

Details in [docs/agent.md](docs/agent.md).

## Runners

```bash
runnerly runner list
runnerly runner status runnerly-01
runnerly runner remove runnerly-01 --purge
```

Labels map straight to GitHub's, so a workflow reaches the runner the usual way:

```yaml
jobs:
  test:
    runs-on: [self-hosted, linux, x64, docker]
```

Details in [docs/runners.md](docs/runners.md) and [docs/github.md](docs/github.md).

## Configuration

```bash
runnerly config init      # write a default config.yaml
runnerly config path      # which file is in use
runnerly config show      # effective settings, defaults included
runnerly config validate  # check it before you rely on it
```

Details in [docs/configuration.md](docs/configuration.md).

## Security

Self-hosted runners execute code from your workflows. Read
[docs/security.md](docs/security.md) before pointing one at a public repository
or at pull requests from forks.

Runnerly refuses to register a runner against a public repository unless you
explicitly allow it, verifies every download against GitHub's published
checksum, and rejects archive entries that would be written outside the install
directory.

## Documentation

- [Getting started](docs/getting-started.md)
- [Installation](docs/installation.md)
- [GitHub integration](docs/github.md)
- [Runners](docs/runners.md)
- [The agent](docs/agent.md)
- [Configuration](docs/configuration.md)
- [Security model](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues and pull requests are welcome.

## License

MIT. See [LICENSE](LICENSE).
