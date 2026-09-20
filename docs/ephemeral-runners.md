# Ephemeral runners

An ephemeral runner accepts one job and is then taken apart. The next job
gets a new registration and a new working directory, so nothing carries over
from the run before it.

```bash
runnerly ephemeral run --repo acme/widgets
```

That performs the whole lifecycle and returns when the job is done:

```text
register → online → one job → cleanup → deregister → destroy
```

## Keeping one fresh runner

Nothing schedules the next run. `ephemeral run` does one job and exits; pair
it with a service manager set to restart it:

```bash
runnerly ephemeral systemd --repo acme/widgets --output runnerly-ephemeral.service
```

`Type=oneshot` with `Restart=always` is the whole loop. The machine always
has exactly one fresh runner, and a failing registration backs off instead of
spinning against GitHub's rate limit.

Deciding *how many machines* to have is autoscaling. Runnerly does not do
that, and the plan says not to until this path is solid.

## Telling a finished job from a crash

This is the part that needs care. An ephemeral runner's `run.sh` exits 0
whether it completed a job or its listener was killed, so the exit code
proves nothing — a crashed runner would otherwise look like a successful one
and be torn down without having done any work.

Runnerly asks the job hooks instead. They record when a job starts and
finishes, so "did this runner actually run something?" has a real answer. A
runner that exits cleanly having run nothing is **restarted**, not retired.

Those hooks are installed by the Docker executor. With `executor.type: host`
there are no hooks, the exit code is all there is, and the old behavior
applies: any clean exit ends the run. That is a real gap, stated rather than
hidden.

## What destroy means

Teardown removes the runner's identity and its working directory:

```text
.runner  .credentials  .credentials_rsa  _work  .runnerly/
```

It leaves the unpacked runner binaries. The clean environment comes from a
fresh registration and a fresh `_work`, not from re-extracting the same
immutable tarball; downloading a few hundred megabytes before every job would
make each one slower for nothing.

Pass `--purge` if you want the directory gone as well, at the cost of a fresh
download next time.

## Deregistering

GitHub removes an ephemeral runner itself once it has run a job, so teardown
usually finds nothing — which is the point of checking. A runner that stopped
*without* running one would otherwise linger in the repository's runner list
as a permanently offline entry, and Runnerly deletes it.

## Logs survive

A destroyed runner takes its own logs with it, which is no use when the thing
you need to debug is the job that just failed. Runnerly writes each run's
output to a file **outside** the runner directory:

```bash
runnerly ephemeral logs
runnerly ephemeral logs eph-a1b2      # filter by name
```

```text
LOG                                SIZE     MODIFIED
20260920-171245-eph-4f3a91c2.log   184 KiB  2026-09-20T17:14:31Z
20260920-170902-eph-8b21ce40.log   91 KiB   2026-09-20T17:10:47Z
```

```yaml
ephemeral:
  log_dir: ""      # empty: a logs directory beside config.yaml
  keep_runs: 50    # 0 keeps every run, and eventually fills the disk
```

This is deliberately a file on the machine, not a feature of the control
plane. Runnerly is not a log platform; if you need logs off the box, point
your existing collector at that directory.

## Lifecycle in the dashboard

A retired runner is not an offline one. Without the distinction, a machine
that finishes a job every few minutes produces a dashboard full of rows that
look like failures.

So an ephemeral runner reports itself retired during teardown:

- its status becomes `retired`, not `offline`
- the runner list hides retired runners unless you tick **include retired**
- the overview counts them separately, and says what the count means
- the event feed keeps `runner_retired` with the job it ran

The row is kept rather than deleted. What a runner did is worth more than the
row costs, and its events point at it.

## Naming

Each run generates a name: the prefix plus random hex, `eph-4f3a91c2`. Reusing
a name would collide with a registration GitHub has not finished removing.

```yaml
ephemeral:
  name_prefix: ""   # empty: the machine's hostname
```

## Configuration

```yaml
ephemeral:
  log_dir: ""
  keep_runs: 50
  name_prefix: ""

executor:
  # Ephemeral works with either. The Docker executor additionally cleans up
  # containers between runs.
  type: docker
```

An ephemeral runner installs the job hooks whatever the executor is. They
are how anything can tell a finished job from a crash, which is the whole
lifecycle of a one-job runner, so it does not get to depend on the executor.

A long-lived runner on the host executor still gets no hooks: it has nothing
to clean up, and the agent reports `online` rather than guessing at `busy`.

## What is not here

- **Autoscaling.** Nothing provisions machines or decides how many runners to
  have. A `Restart=always` unit keeps one fresh runner on a machine you
  already have; deciding how many machines to have is a different problem.
- **A pool on one machine.** One `ephemeral run` supervises one runner. Run
  several units with different name prefixes and directories if you want more.
- **VM-per-job isolation.** Still the honest answer to "I need a real
  boundary"; see [security.md](security.md).
- **Log forwarding.** Logs are kept on the machine. Shipping them elsewhere
  is your collector's job.
