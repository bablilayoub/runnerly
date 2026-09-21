# Changelog

Notable changes to Runnerly. Versions follow [semantic versioning](https://semver.org),
and while the major version is 0 the minor version is where breaking changes
land.

## Unreleased

## 0.1.0

The first release. Everything below is new, so the list is what Runnerly
does rather than what changed.

### Setting a machine up

- `runnerly setup` does the whole thing in one command: checks the machine,
  signs in to GitHub, picks a repository, registers a runner and prints the
  commands that keep it running. It shows the full plan and waits before
  changing anything, and `--repo --yes` makes it non-interactive for
  provisioning.
- `runnerly doctor` reports what would stop a runner working — Docker, disk,
  network, credentials, scopes — and names the command that fixes each
  failure. Exits non-zero, so it gates a provisioning script. `--json` for
  machine-readable output, `--offline` to skip the network.
- `install.sh` detects the platform, verifies the release archive against the
  published checksum, and installs three binaries. It never calls `sudo` on
  your behalf.

### Runners

- `runnerly runner create` installs GitHub's official runner, verifying the
  SHA-256 checksum GitHub publishes with it and refusing any archive entry
  that would be written outside the install directory.
- Registration against a public repository is refused unless explicitly
  allowed, because a self-hosted runner executes whatever a workflow asks of
  it.
- `runnerly upgrade` reports what is out of date and, with `--runners`,
  brings this machine's runners to the release GitHub expects. It skips a
  runner that is mid-job.

### Keeping them running

- `runnerly agent run` supervises a runner and restarts it on a 5s, 10s, 20s,
  40s, 80s backoff, then stops rather than hiding one that cannot start. A
  second limit of ten restarts an hour catches failures slow enough that each
  one looks like a healthy run.
- `runnerly agent systemd` prints a unit and the commands to install it. It
  installs nothing itself.
- The Docker executor removes the containers, volumes and networks a job
  created, and only those: it records what existed when the job started
  rather than pruning.
- Job hooks report `busy` from the runner itself rather than inferring it
  from the process being alive.

### One-job runners

- `runnerly ephemeral run` registers a runner, lets it take one job, cleans
  up, deregisters and destroys it. Logs are kept outside the runner directory
  so they outlive it.

### A fleet

- `runnerly-server` is an optional control plane: enrollment, heartbeats,
  events, an API and a dashboard served from the same binary. A single
  machine never needs one.
- Agents enroll with a one-shot token and trade it for a machine token the
  server rotates.
- Restart can be requested from the dashboard; it rides the heartbeat,
  because the control plane cannot reach a machine behind NAT.
- TLS, rate limiting, Prometheus metrics and an audit trail. Credentials the
  server only has to recognize are hashed; the one it must read back is
  encrypted.

### Platforms

- Linux x86-64 and arm64 are developed and tested.
- macOS runs runners, but has no service integration: the generated unit is
  systemd.
- Windows is not supported — the job hooks are shell scripts.

### Known limits

Written down rather than left to be discovered: no autoscaling or cloud
provisioning, no VM-per-job isolation, no unattended upgrades, no
self-replacing binary, no package manager, and no multi-tenancy in the
dashboard. See the "Not built" section of the README.
