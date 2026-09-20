# Getting started

Runnerly is in early development. This page covers what works today.

## What Runnerly does

GitHub schedules your workflows and assigns jobs. A *self-hosted runner* is a
machine you own that picks up those jobs. Running one by hand means downloading
the runner, finding a registration token, running `config.sh`, writing a systemd
unit, and then noticing three weeks later that it went offline.

Runnerly is the layer around that: it installs, registers, monitors and
retires runners so you do not do those steps by hand.

## What works today

- `runnerly doctor` — tells you whether a machine can host a runner
- `runnerly login` — stores a GitHub token
- `runnerly repo list` — finds repositories you can attach a runner to
- `runnerly runner create` — installs and registers a real runner
- `runnerly runner list` / `status` / `remove` — manages registrations
- `runnerly config` — creates and inspects the configuration file
- `runnerly version` — build information

Runnerly does not yet start or supervise the runner process. After
`runner create`, you run `./run.sh` yourself. See
[architecture.md](architecture.md) for the plan.

## Install

See [installation.md](installation.md). There is no release build yet, so you
build from source:

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make build
sudo make install     # optional: puts runnerly in /usr/local/bin
```

## Check a machine

```bash
runnerly doctor
```

Every line is a check. Passing checks stay quiet; anything else explains itself
and suggests a fix:

```text
✗ docker daemon
  Docker is installed but the daemon did not respond.
  Cannot connect to the Docker daemon at unix:///var/run/docker.sock.
  Try:
    sudo systemctl start docker
```

Useful flags:

| Flag | Effect |
| --- | --- |
| `--json` | machine-readable report, for provisioning scripts |
| `--offline` | skip every check that needs the network |
| `--timeout` | per-check timeout (default 5s) |
| `--no-color` | plain output |

`doctor` exits `0` when nothing failed and `1` otherwise, so it can gate a
provisioning step:

```bash
runnerly doctor --json > /var/log/runnerly-doctor.json || exit 1
```

Warnings do not fail the run. Running on macOS, for example, warns that
Runnerly only manages Linux runners but still exits `0` so you can develop
against the CLI.

## Create a configuration

```bash
runnerly config init
runnerly config path      # where it went
runnerly config show      # effective settings, defaults included
```

Then edit it and check your work:

```bash
runnerly config validate
```

Every field is documented in [configuration.md](configuration.md).

## Authenticate with GitHub

Create a personal access token with the `repo` scope, then:

```bash
echo "$GITHUB_TOKEN" | runnerly login --with-token
runnerly auth status
```

The token is read from standard input, not a flag, so it stays out of your
shell history. To keep it off disk entirely, set `RUNNERLY_GITHUB_TOKEN`
instead — an environment token always wins.

## Register a runner

```bash
runnerly repo list
runnerly runner create --repo owner/repo
```

Runnerly asks GitHub for a short-lived registration token, downloads the runner
release GitHub expects, verifies its checksum, unpacks it, and registers it.
Then start it:

```bash
cd ~/.local/share/runnerly/runners/<name> && ./run.sh
```

Push a workflow that targets it:

```yaml
jobs:
  test:
    runs-on: [self-hosted, linux, x64]
    steps:
      - uses: actions/checkout@v4
      - run: echo "running on $(hostname)"
```

Runnerly refuses to register against a public repository by default. Read
[security.md](security.md) before overriding that.

## Next

- [GitHub integration](github.md)
- [Runners](runners.md)
- [Configuration](configuration.md)
- [Security model](security.md) — read this before pointing a runner at a public repository
- [Troubleshooting](troubleshooting.md)
