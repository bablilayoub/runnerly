# The dashboard

A web interface for the control plane: what runners exist, whether they are
healthy, what has happened, and the two actions worth having a button for.

It is served by `runnerly-server` itself, embedded in the binary. There is no
second process to deploy and no static host to configure.

```text
https://runnerly.example.com/          the dashboard
https://runnerly.example.com/api/v1/   the same API the agents use
```

## Signing in

GitHub OAuth, and Runnerly ships no client credentials — a GitHub OAuth App
belongs to whoever runs the server. Until one is configured the sign-in page
says exactly which settings are missing rather than offering a button that
fails. See [server.md](server.md#dashboard-sign-in).

Agents never sign in. They enroll with a token and keep reporting whether or
not anyone has the dashboard open.

## Pages

**Overview** — counts across the fleet and the most recent events. It calls
out runners that have gone stale, because a runner that stopped reporting but
has not yet crossed the offline threshold is the case worth noticing early.

**Runners** — every runner the control plane knows about, with status, scope,
platform, labels and how long ago each last reported.

Retired runners are hidden unless you ask for them. An ephemeral runner
retires after its job, so a busy machine would otherwise bury the runners
actually in service under a list of completed ones.

This is Runnerly's view, not GitHub's. A runner here has enrolled with an
agent; a runner in `runnerly runner list` is registered with GitHub. Usually
the same machines, but not by definition, and the dashboard does not pretend
otherwise.

**Runner detail** — hardware, reporting, labels, recent commands and the
runner's own event history, plus Restart and Remove.

**Events** — the whole feed, filterable by severity. The control plane is not
a log platform: this is pruned on a schedule, so it is recent history rather
than an archive.

**Settings** — the signed-in account, and the enrollment tokens machines use.
Creating one here shows the secret once, with the exact commands to run on the
machine.

## What the load figures mean

The runner page reports processor, memory and disk use on every heartbeat.
Each is a percentage from 0 to 100, and each is a dash when the machine has
no way to measure it — unknown is shown as unknown rather than as zero.

**Processor** is time spent doing anything other than idling, across all
cores. On Linux it is the difference between two readings of `/proc/stat`,
so the window is the heartbeat interval and the figure is an average over
it rather than a spot reading. On macOS there is no counter to difference
without cgo, so it is a one-second sample taken with `iostat`.

**Memory** is what is committed, not what is unfree. On Linux that is
`MemAvailable`: the kernel's own estimate of what a new workload could get.
Page cache does not count as used, which matters because on any machine
that has been up a while almost nothing is free and reporting `MemFree`
would show every healthy build machine at 97%. On macOS it is the figure
Activity Monitor calls Memory Used — anonymous pages, less purgeable, plus
wired plus the compressor — so it can be checked against the machine.

Inside a container with a cgroup v2 memory limit, both the total and the
percentage are the container's, not the host's. A runner capped at 2 GiB is
a 2 GiB machine as far as anything it runs is concerned. Processor use is
still the host's: `/proc/stat` is not namespaced.

**Disk** is how full the filesystem holding the runner directory is, with
space reserved for root counted as used, because a build cannot write to
it. On Linux this is the number `df` prints. On macOS it is not: `statfs`
describes the whole APFS container while `df` splits it per volume, so a
460 GiB disk with 235 GiB free reads as 49% full here and 46% in `df`. The
first is the one that answers whether a build will fit.

Readings are taken on their own schedule and the heartbeat sends whichever
is latest, so a slow measurement never delays a report.

## Restart

Restart asks the agent to stop the runner and start it again. The agent sends
SIGTERM first, so a job already running gets to finish.

**It is queued, not immediate.** The server cannot reach an agent — runner
machines sit behind NAT and firewalls, and opening an inbound port on each one
would be worse than waiting — so the command rides along on the next
heartbeat. With the default interval that is within about twenty seconds. The
dashboard says so rather than implying the button was instant, and the
Commands table on the runner page shows `pending`, then `delivered`, then
`done` or `failed`.

Pressing it twice queues one restart, not two. An operator-requested restart
does not count against the agent's backoff: asking for one should not push a
runner closer to the point where the agent gives up on it.

A command no agent collects within an hour is marked failed, so nothing sits
`pending` for ever after a machine goes away.

## Remove

Remove forgets a runner **in the control plane only**. It does not deregister
it with GitHub and does not touch the machine, so a runner whose agent is
still running will reappear on its next heartbeat. The confirmation says this,
and points at `runnerly runner remove <name> --purge` for retiring one
properly.

## Design

Monochrome, dense, high contrast. Color appears in exactly one place — runner
status and event severity — because that is where hue carries meaning rather
than decoration.

Numbers use tabular figures so columns line up. Tables scroll horizontally on
a narrow screen rather than reflowing into something unreadable. Polling
updates data in place instead of blanking the page, and stops while the tab is
hidden.

## Building it

```bash
make web      # builds the dashboard into internal/web/dist
make build    # builds the binaries, embedding whatever is there
make all      # both, in that order
```

`make build` alone produces a working server **without** the dashboard: the Go
build never requires Node. Such a binary serves a page explaining that and
says how to get one, and the API is unaffected. Release binaries include it.

For development, run the server and Vite side by side:

```bash
runnerly-server                 # terminal one
cd web && npm run dev           # terminal two, proxies /api to the server
```

Set `RUNNERLY_SERVER_URL` if the server is not on `127.0.0.1:8080`.

## What is not here

- **Job history.** GitHub owns that, and Runnerly is not going to duplicate
  it. The dashboard covers machines.
- **Log streaming.** The event feed is not a log viewer, and the agent's logs
  stay on the machine.
- **Remote upgrades.** `runnerly upgrade --runners` works on the machine
  itself; asking for one from here does not. The command channel could carry
  it, and the missing part is not plumbing — an upgrade restarts a runner,
  and nothing here knows when a machine is safe to interrupt. See
  [operations.md](operations.md#upgrades).
- **Access control beyond the allow list.** Everyone who can sign in sees
  everything.
