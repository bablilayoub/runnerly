# Changelog

Notable changes to Runnerly. Versions follow [semantic versioning](https://semver.org),
and while the major version is 0 the minor version is where breaking changes
land.

## Unreleased

## 0.1.3

### Added

- **Windows has the platform pieces it was missing**, and a
  `windows-latest` CI job that checks them on a real Windows machine
  rather than by cross-compiling. The release unpacks from its zip, the
  job hooks are a `.ps1`, a graceful stop is a console control event, a
  secret is read through the console API, the machine readings come from
  kernel32, and `agent schtasks` prints a scheduled task — a task rather
  than a service, because the Service Control Manager would start a
  console program, wait for a reply that never came, and kill it. It is
  still **not a supported platform**: there is no installer and nobody
  has watched a Windows runner take a job. [docs/windows.md](docs/windows.md)
  is the honest list.
- **macOS is supported rather than tolerated.** `runnerly agent launchd`
  and `runnerly ephemeral launchd` write the LaunchAgent that is a Mac's
  equivalent of a systemd unit, and `doctor` stops warning about the
  platform. An agent rather than a daemon, deliberately: a daemon has no
  login session, and a Mac build usually needs one for the keychain and
  code signing — which is also why the machine has to log in for the
  runner to come back after a reboot, and the printed instructions say so.
  The plist sets a PATH that includes both Homebrew prefixes, because
  launchd's minimal one is how a runner that works in a terminal fails as
  a service.
- `doctor` warns when Runnerly or the runner directory is inside a folder
  macOS guards — Desktop, Documents, Downloads, iCloud Drive. Started by
  launchd from one of those, the agent does not fail: it blocks inside the
  dynamic linker waiting for a consent prompt no background job can show,
  and `launchctl print` cheerfully reports `state = running` with a pid
  and an empty log for as long as you leave it. This was found by running
  it, not by reading about it.
- `make lint-platforms` runs the linter once for every platform Runnerly
  compiles for. Build tags hide files from the linter as surely as from
  the compiler, so a clean local run had been checking only the files that
  build on the machine running it.

### Changed

- The dashboard is rebuilt on [shadcn/ui](https://ui.shadcn.com). It looks
  like the same dashboard, deliberately — monochrome, dense, status the
  only coloured thing — but the parts underneath that have to handle focus,
  Escape, click-outside and arrow keys are Radix's rather than hand-rolled.
  It also gains the logo, a light theme that follows the operating system
  until you pick one, toasts in place of inline notices, skeletons in place
  of the word "Loading", copy buttons on the enrollment commands, and bars
  rather than bare numbers for processor, memory and disk. Every colour was
  measured against its background; amber had to come down a step to clear
  4.5:1 in the light theme.
- The runners list shows each machine's load as three small bars, so the
  one that is working is visible while scanning rather than one page in.

### Fixed

- A filter on the dashboard took effect on the next poll rather than at
  once, so picking a severity highlighted the button and left the
  unfiltered list on screen for five seconds. Same for "include retired",
  and for following a link from one runner's page to another's, which
  showed the previous runner's machine until the poll caught up.
- A row in the runners list could only be opened with a mouse. The name
  is a link now; the row click stays as a convenience on top of it.
- `setup` and `runner create` finished by telling you to run
  `runnerly agent systemd`, whatever machine you were on. On a Mac that
  is an instruction for a service manager that is not there.
- Issuing or revoking an enrollment token left the audit trail on the same
  page saying nothing had been recorded, directly underneath the thing that
  had just been recorded.

## 0.1.2

### Added

- The runner page's processor, memory and disk figures are measured and
  reported. They were rendered from fields nothing ever set, so the panel
  read "—" on every runner. Processor use is the difference between two
  readings of `/proc/stat` on Linux and a one-second `iostat` sample on
  macOS, where there is no counter to difference without cgo. Memory is
  what is committed rather than what is unfree — `MemAvailable` on Linux,
  the figure Activity Monitor calls Memory Used on macOS — because
  reporting free memory shows every healthy build machine at 97%. Inside a
  container with a cgroup v2 memory limit, the limit is the total.
  Readings are taken on their own schedule and the heartbeat carries the
  latest, so measuring never delays a report. A platform that cannot
  measure something still reports nothing rather than zero.

### Fixed

- `runnerly setup` ended the command when you picked a public repository,
  telling you to start again with `--allow-public` — after its own picker
  had offered the repository. The refusal is right, because a runner on a
  public repository executes whatever a pull request asks of it, but it is
  now delivered as a question to the person standing there. Declining goes
  back to the list. A scope named with `--repo` is still refused outright,
  and `--yes` still requires `--allow-public`.
- The runner page's Memory and Disk rows were always a dash. Both fields
  were declared, transmitted, stored and rendered, and nothing set them.
- The agent logged "reporting is stopping" after the control plane refused
  its credential and then carried on retrying every heartbeat, forever.
  The runner does keep running, which is deliberate; the reporting really
  stops now, and the message says how to restore it.
- `runnerly doctor` reported a bare Ubuntu machine as ready and `setup`
  then failed at `config.sh` with "Libicu's dependencies is missing for
  Dotnet Core 6.0", a message naming neither Runnerly nor the package.
  doctor checks for libicu and names it.
- The overview said "1 runner have not reported ... They are still
  counted".

## 0.1.1

### Fixed

- `runnerly setup` refused to continue on an account with more than thirty
  administrable repositories, telling you to pass `--repo` instead. The cap
  was a refusal rather than a page size, and it was the first thing anyone
  with a lot of repositories hit. The picker now shows the twenty most
  recently pushed, says how many more there are, and takes a number, a
  search to narrow the list, or a full `owner/repo` typed straight in. A
  number outside the list is re-asked rather than ending the command.
- Two prompts in `setup` each built their own buffered reader over the same
  standard input, so one question could swallow the answer to the next.
  Only reachable once the wizard asked more than once, which the repository
  search made it do.

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
