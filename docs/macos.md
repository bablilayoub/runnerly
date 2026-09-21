# Running runners on macOS

Runners register, take jobs, clean up after them and come back after a
reboot on a Mac. What is different from Linux is the service manager, and
two things macOS does that nothing else does.

Everything in [agent.md](agent.md) applies. This page is only the
differences.

## The service is a LaunchAgent

```bash
runnerly agent launchd            # prints the property list
runnerly agent launchd --output dev.runnerly.agent.<name>.plist
```

`agent systemd`'s equivalent. It writes nothing outside the path you give
it and loads nothing; the commands to install it are printed alongside and
none of them need root.

```bash
mkdir -p ~/Library/LaunchAgents ~/Library/Logs/runnerly
cp dev.runnerly.agent.<name>.plist ~/Library/LaunchAgents/
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.runnerly.agent.<name>.plist
```

Stop it with `launchctl bootout gui/$(id -u)/dev.runnerly.agent.<name>`,
which sends SIGTERM, so the runner finishes the job it is on.

For one-job runners there is `runnerly ephemeral launchd`, which is the
same loop `ephemeral systemd` runs: the command takes one job and exits,
launchd starts another.

**A LaunchAgent, not a LaunchDaemon.** A daemon starts at boot with no
login session, and on a Mac that costs a build the login keychain, code
signing, and anything that needs a window server — which is most of why
anyone runs CI on a Mac. GitHub's own runner installs itself as a
LaunchAgent for the same reason.

The price is that **the machine has to log in**. A build host that should
come back on its own after a reboot needs automatic login, in System
Settings > Users & Groups > Login Options. Without it the runner comes back
when someone next logs in, and not before.

## Protected folders will hang the agent silently

macOS guards `~/Desktop`, `~/Documents`, `~/Downloads` and iCloud Drive
behind a consent prompt. A process launchd starts cannot show that prompt,
and it does not fail — it blocks, inside the dynamic linker, before running
a single line of its own code.

What that looks like:

```
$ launchctl print gui/$(id -u)/dev.runnerly.agent.mac-mini | grep state
	state = running
```

A pid, a job reporting itself as running, an empty log file, and nothing
ever happening. Nothing in the symptom points at the cause.

Keep the binaries and the runner directory out of those four folders.
`install.sh` already does — `/usr/local/bin` and `~/.local/bin` are not
guarded — and `runnerly doctor` warns if either has ended up somewhere that
is:

```
! protected folders
  Runnerly itself is in ~/Desktop.
  Run by launchd, a process reading one of these blocks on a consent prompt
  it cannot show: the job reports itself running and does nothing at all.
```

## PATH

launchd hands a job a minimal `PATH`, with no Homebrew and no
`/usr/local/bin`, and the agent passes its own environment to the runner.
A workflow calling anything installed with `brew` then fails with "command
not found" on a machine where it is plainly installed.

The generated plist sets a `PATH` that includes both Homebrew prefixes.
Override it with `--path` if your machine needs something else.

## Logs

There is no journal, so the plist sends the agent's output to
`~/Library/Logs/runnerly/<name>.log`:

```bash
tail -f ~/Library/Logs/runnerly/<name>.log
```

Nothing rotates it. The agent logs transitions rather than job output, so
it grows slowly, but on a long-lived machine it does grow. Add a
`newsyslog.d` entry if that matters to you, or point `--log-file` at
somewhere your existing rotation already covers.

## Labels

A Mac registers `macOS` and `ARM64` — GitHub's names, which is what a
workflow's `runs-on` matches. See
[configuration.md](configuration.md#defaults-worth-knowing).

## What is not different

- Docker cleanup between jobs works, with Docker Desktop or Colima.
- The job hooks are shell scripts, and macOS runs shell scripts.
- `doctor`, `setup`, `runner create`, `upgrade` and the control plane agent
  all behave the same.
