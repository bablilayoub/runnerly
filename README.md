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

Early development. Today the CLI can tell you whether a machine is ready to
host a runner, and manage its configuration file.

| Area | State |
| --- | --- |
| `runnerly version` | works |
| `runnerly doctor` | works |
| `runnerly config` | works |
| `runnerly setup`, GitHub registration | not implemented |
| Runner agent, control plane, dashboard | not implemented |

Nothing here registers a runner with GitHub yet. See
[docs/architecture.md](docs/architecture.md) for where this is going.

## Try it

Requires Go 1.24 or newer.

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make build
./dist/runnerly doctor
```

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

## Documentation

- [Getting started](docs/getting-started.md)
- [Installation](docs/installation.md)
- [Configuration](docs/configuration.md)
- [Security model](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Architecture](docs/architecture.md)
- [Development](docs/development.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues and pull requests are welcome.

## License

MIT. See [LICENSE](LICENSE).
