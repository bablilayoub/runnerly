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
- **Upgrades.** `POST /runners/{id}/upgrade` is in the plan; the command
  channel now exists to carry it, but nothing implements it yet.
- **Access control beyond the allow list.** Everyone who can sign in sees
  everything.
