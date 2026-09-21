# Getting started

The shortest path from a bare machine to a runner picking up jobs.

## What Runnerly does

GitHub schedules your workflows and assigns jobs. A *self-hosted runner* is a
machine you own that picks up those jobs. Running one by hand means downloading
the runner, finding a registration token, running `config.sh`, writing a systemd
unit, and then noticing three weeks later that it went offline.

Runnerly is the layer around that: it installs, registers, monitors and
retires runners so you do not do those steps by hand.

## The commands

| Command | What it does |
| --- | --- |
| `runnerly setup` | all of the below, in one interactive command |
| `runnerly doctor` | says whether a machine can host a runner |
| `runnerly login` / `logout` / `auth status` | the GitHub token |
| `runnerly repo list` | repositories you can attach a runner to |
| `runnerly runner create` | installs and registers a real runner |
| `runnerly runner list` / `status` / `remove` | the registrations in GitHub |
| `runnerly agent run` | supervises the runner and restarts it when it fails |
| `runnerly agent systemd` / `status` | a service unit; what is installed here |
| `runnerly ephemeral run` | register, one job, clean up, destroy |
| `runnerly upgrade` | what is out of date, and `--runners` to fix it |
| `runnerly server` | the control plane, its tokens and its keys |
| `runnerly config` | creates and inspects the configuration file |
| `runnerly version` | build information |

Everything up to `agent run` works on one machine with no server. A control
plane is optional and covered at the end.

## The short version

```bash
curl -fsSL https://runnerly.dev/install.sh | sh
runnerly setup
```

`setup` checks the machine, signs in to GitHub, picks a repository, registers
a runner and prints the commands to keep it running — showing the whole plan
and waiting before it changes anything.

That is the whole page. The rest of it is what `setup` is doing, one step at
a time, because sooner or later you will want to do one of them by hand.

```bash
runnerly setup --repo owner/repo --yes   # for a provisioning script
```

With `--yes` it prompts for nothing and fails rather than asking, so supply
the token through `RUNNERLY_GITHUB_TOKEN`.

If you do not pass `--repo`, it lists the repositories your token can
administer, most recently pushed first. On an account with more than a
screenful, type part of a name to narrow the list, or type the full
`owner/repo` to skip the list entirely.

## Install

The installer needs a published release. Without one — or on a repository
that is private to you — build from source:

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make all              # the dashboard, then the binaries
sudo make install     # optional: puts them in /usr/local/bin
```

`make build` on its own skips the dashboard and needs no Node. The CLI and
the agent do not use it either way. See [installation.md](installation.md).

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

Warnings do not fail the run. On macOS, for example, `doctor` warns that
`agent systemd` generates a unit macOS does not use — runners themselves work
there — and still exits `0`.

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

## Keep it running

```bash
runnerly agent run
```

The agent starts the runner and restarts it if it fails, backing off 5s, 10s,
20s, 40s, 80s. Ctrl-C stops both cleanly, giving the runner a chance to finish
the job it is on.

To run it at boot, generate a systemd unit and install it yourself:

```bash
runnerly agent systemd --output runnerly-agent.service
```

See [agent.md](agent.md) for the install commands and what the unit does.

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

## A clean machine for every job

A long-lived runner accumulates whatever its jobs leave behind. An ephemeral
one takes a single job and is then taken apart:

```bash
runnerly ephemeral run --repo owner/repo
```

Under a `Restart=always` unit, that gives the machine a fresh runner for
every job. See [ephemeral-runners.md](ephemeral-runners.md).

## Several machines

Once more than one machine runs a runner, an optional control plane answers
"what is out there and is it healthy?", with a dashboard to look at it.
Nothing above requires it.

```bash
docker compose -f deploy/compose/docker-compose.yml up -d
runnerly server enrollment-token create --max-uses 1
```

Then on each runner machine, before `agent run`:

```bash
export RUNNERLY_SERVER_URL=https://runnerly.example.com
export RUNNERLY_ENROLLMENT_TOKEN=rnr_enroll_...
```

The agent trades that one-shot token for a machine token of its own. See
[server.md](server.md) and [dashboard.md](dashboard.md).

## Next

- [Installation](installation.md) — platforms, cross-compiling, uninstalling
- [GitHub integration](github.md)
- [Runners](runners.md)
- [The agent](agent.md)
- [The Docker executor](docker.md)
- [Ephemeral runners](ephemeral-runners.md)
- [The control plane](server.md)
- [The dashboard](dashboard.md)
- [Running in production](operations.md) — TLS, metrics, backups, upgrades
- [Configuration](configuration.md)
- [Security model](security.md) — read this before pointing a runner at a public repository
- [Troubleshooting](troubleshooting.md)
