# Windows

**Windows is not a supported runner platform yet.** This page says what
works, what does not, and how the parts that do work are checked, because
"not supported" on its own tells you nothing about how far off it is.

## What works

Every piece below is exercised by CI on a real `windows-latest` machine,
not cross-compiled and assumed.

- **The machine readings.** Memory, disk and processor use come from
  `GlobalMemoryStatusEx`, `GetDiskFreeSpaceExW` and `GetSystemTimes`.
- **Unpacking the release.** GitHub ships a zip for Windows rather than a
  tar.gz, and `runner create` reads both, with the same refusal to write
  anything outside the directory it was given.
- **The job hooks.** They are a `.ps1` here. GitHub's runner starts
  exactly two kinds — it reads the extension and hands a `.sh` to bash and
  a `.ps1` to PowerShell — so a batch file would never run at all. CI
  invokes the generated hook the way the runner does,
  `pwsh -command ". '<path>'"`, against a real compiled executable, and
  checks that a path with a space and an apostrophe arrives intact.
- **Stopping a runner gracefully.** There are no signals, so the agent
  sends `CTRL_BREAK_EVENT` to the runner's process group. CI checks that
  the event reaches the runner and does not reach the agent.
- **A scheduled task.** `runnerly agent schtasks` prints one; CI imports
  it into the real Task Scheduler, which is the only thing that can say
  whether the document is valid.

One thing is implemented and **not** in that list: reading a secret
without echoing it, through the console API rather than `stty`. Turning
echo off needs a real console and CI has none, so it is written and
compiled and nobody has watched it work.

## What does not

- **Nobody has watched a Windows runner take a real job.** That is the
  one that matters, and until it happens the rest is parts rather than a
  product.
- **There is no installer, and no published binaries.** `install.sh` is a
  POSIX shell script, and the release workflow builds Linux and macOS
  only. Build from source: `go build ./cmd/...`.
- **The Docker executor is untried here.** It shells out to `docker`, so
  it may well work; nobody has run it.
- **A credentials file is protected by its ACL, not its mode.** Runnerly
  writes it 0600, and Windows has no such thing: Go turns the mode into
  the read-only attribute and the file inherits whatever its directory
  allows. In practice that directory is inside the user's profile, which
  is already private — but Runnerly is not the thing making it so. See
  [security.md](security.md).

## Why a scheduled task and not a service

A Windows service has to be written to answer the Service Control
Manager. `runnerly-agent` is a console program: the Service Control
Manager would start it, wait for a reply that never came, and kill it as
unresponsive.

A scheduled task runs a console program at boot and restarts it when it
fails, which is what a systemd unit does for the same program on Linux.

```bash
runnerly agent schtasks --output runnerly-agent.xml
schtasks /create /xml runnerly-agent.xml /tn "Runnerly\runnerly-agent-<name>" /ru "%USERDOMAIN%\%USERNAME%" /rp *
```

The account is supplied on the import rather than written into the file,
and `/rp *` prompts, so no password ends up on disk or in the console
history.

Two defaults are changed and both matter: the execution time limit is
removed, because the default stops the task after three days — a machine
that quietly stops taking jobs every three days — and the task restarts
itself on failure, because the agent restarts the runner and something has
to restart the agent.

## How this is checked

CI runs a `windows-latest` job on every push: it builds, runs the whole
test suite, smoke-tests the CLI, and then fails if the Windows-only tests
skipped rather than ran. That last check exists because a build tag going
wrong would silently remove the only real verification this platform gets,
and a skipped suite looks exactly like a passing one.

Cross-compiling proves a file parses. It says nothing about whether a
struct handed to the kernel has the layout the kernel expects, which is
the mistake this kind of code actually makes.
