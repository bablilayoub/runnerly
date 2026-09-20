# The agent

`runner create` registers a runner. The agent is what keeps it running.

```text
systemd
   │  restarts the agent if the agent dies
   ▼
runnerly-agent
   │  restarts the runner if the runner dies
   ▼
run.sh  →  Runner.Listener  →  your workflow jobs
```

Both layers are needed. The agent handles a runner that crashes; the service
manager handles an agent that crashes.

## Running it

In the foreground, which is the easiest way to see what happens:

```bash
runnerly agent run
```

The name is optional on a machine with one runner. With several:

```bash
runnerly agent run runnerly-01
```

Stop it with Ctrl-C. The agent sends the runner SIGTERM, which lets it finish
the job it is on, and waits before killing it.

## What it does when the runner dies

```text
runner exits
     ↓
agent notices
     ↓
wait 5s  →  restart
     ↓ still failing
wait 10s →  restart
     ↓
20s, 40s, 80s
     ↓ still failing
give up, log runner_failed, exit non-zero
```

The agent stops rather than restarting forever, because a runner that cannot
start is a problem to surface, not to hide. systemd then restarts the agent,
which tries the whole schedule again — slowly enough not to hammer GitHub.

A runner that stays up for a minute has its failure history cleared, so a
machine that works for weeks is never one crash away from the end of its
backoff.

### Why there is also a ceiling

Clearing the history on uptime alone is not enough, because **uptime is not
health**. A runner can stay up for four minutes failing and then exit, and
four minutes is longer than the minute that earns a clean slate — so every
failure looked like a healthy run that happened to end, the count reset, and
the five-restart limit was never reached.

That is a real failure, not a hypothetical one. Kill a runner with `SIGKILL`
and it never tells GitHub it has gone, so the replacement is refused:

```text
A session for this runner already exists.
Stop retry on SessionConflictException after retried for 240 seconds.
Runner listener exit with Session Conflict error, stop the service.
```

It exits `0`, after 240 seconds. Left alone, the agent restarted it forever.

So a second limit applies that uptime cannot clear: **no more than ten
restarts in an hour**, however healthy the runs in between looked. Ten an
hour is far above a working machine — a fine runner restarts when GitHub
ships an update, days apart — and far below a loop, which manages one every
few minutes. Past it the agent gives up and says why, and systemd's own
restart limits take over from there.

The session conflict itself clears on GitHub's side after a few minutes, so
the next agent start succeeds. The point of the ceiling is that the failure
becomes visible instead of silent.

## Logs

The agent writes structured logs on stdout and forwards the runner's own
output to stderr. They are kept apart because the runner's output is not
structured, and interleaving them would stop a collector parsing either.

```bash
runnerly agent run --log-format json
runnerly agent run --log-level debug
runnerly agent run --quiet-runner    # drop the runner's own output
```

```json
{"time":"2026-09-20T03:23:39Z","level":"INFO","msg":"runner is running",
 "component":"runner-agent","runner":"runnerly-01","scope":"acme/widgets",
 "event":"runner_online","pid":9859}
```

Every line carries `time`, `level`, `component`, `runner` and `event`. Filter
on `event` for a specific transition, or on `level` for anything wrong.

| Event | Level | Meaning |
| --- | --- | --- |
| `agent_started` | INFO | the agent is up |
| `runner_online` | INFO | the runner process is running |
| `runner_exited` | WARN | the runner stopped when it should not have |
| `runner_restarting` | WARN | waiting out the backoff |
| `runner_healthy` | INFO | it stayed up long enough to clear its history |
| `runner_failed` | ERROR | the agent gave up |
| `runner_stopping` | INFO | a shutdown was asked for |
| `runner_killed` | WARN | a graceful stop timed out |
| `runner_offline` | INFO | the runner is gone |
| `agent_stopped` | INFO | clean shutdown |

**`runner_exited` is a warning even when the exit code is 0.** Killing the
runner's listener makes `run.sh` exit 0, so a crash and a clean stop cannot be
told apart by exit code. A runner meant to stay up should never exit at all,
so any exit is worth your attention.

## Running it as a service

```bash
runnerly agent systemd runnerly-01
```

That prints a unit for this machine. It installs nothing: putting a file in
`/etc` and enabling a service needs root, and Runnerly does not take
privileges you did not hand it. Read the unit, then run the commands it prints:

```bash
runnerly agent systemd runnerly-01 --output runnerly-agent.service
sudo useradd --system --home <dir> --shell /usr/sbin/nologin runnerly
sudo chown -R runnerly:runnerly <dir>
sudo install -m 0644 runnerly-agent.service \
  /etc/systemd/system/runnerly-agent@runnerly-01.service
sudo systemctl daemon-reload
sudo systemctl enable --now runnerly-agent@runnerly-01
journalctl -u runnerly-agent@runnerly-01 -f
```

The unit runs as a dedicated `runnerly` user, never root. Change it with
`--user` and `--group`. A reference copy lives in
[`deploy/systemd/runnerly-agent.service`](../deploy/systemd/runnerly-agent.service).

`TimeoutStopSec` defaults to three minutes so a job in flight has a chance to
finish on stop. Raise it with `--stop-timeout` if your jobs run longer.

## What is installed here

```bash
runnerly agent status
```

```text
NAME         SCOPE          READY  DIRECTORY
runnerly-01  acme/widgets   yes    /home/me/.local/share/runnerly/runners/runnerly-01
```

This reads local state only and never contacts GitHub, so it answers a
different question from `runnerly runner list`, which reports what GitHub has
registered. A runner can be `READY` here and offline there, which just means
nothing is running it.

`READY no` comes with the reason — usually a missing `.runner`, meaning the
directory was never registered, or a directory that has been deleted.

## Where state lives

`runners.yaml`, beside `config.yaml`:

```yaml
runners:
  runnerly-01:
    name: runnerly-01
    scope: {kind: repository, owner: acme, repo: widgets}
    host: github.com
    dir: /home/me/.local/share/runnerly/runners/runnerly-01
    labels: [self-hosted, linux, x64, runnerly]
    installed_at: 2026-09-20T03:23:21Z
```

It is what the operator asked for, separately from `config.yaml`, which is
what they configured. Keeping them apart means installing a runner never
rewrites a hand-edited file, and it is why `runner list`, `runner remove` and
the agent no longer need `--repo` on a machine that has one runner.

It holds no secrets. The runner's own credentials are written by GitHub's
`config.sh` inside the runner directory, and Runnerly never copies them.

## Reporting to a control plane

With `server.url` set, the agent also reports to a Runnerly control plane: it
enrolls once, then sends heartbeats and forwards the same events it logs.

```bash
export RUNNERLY_SERVER_URL=https://runnerly.example.com
export RUNNERLY_ENROLLMENT_TOKEN=rnr_enroll_...
runnerly agent run
```

The enrollment token is one-shot and is traded for a machine token stored in
`credentials.yaml`; after that the agent needs neither. See
[server.md](server.md).

Reporting never gets in supervision's way. Events queue in a buffer and the
oldest are dropped if the control plane cannot keep up, with the count logged.
A failed heartbeat is logged and retried on the next tick. A control plane
that is down does not take a working runner with it.

The agent reports `starting`, `online`, `busy`, `stopping`, `offline` and
`error`.

`busy` comes from the job hooks, which the runner itself calls when a job
starts and finishes. The Docker executor installs them, because it needs
them for cleanup too. With `executor.type: host` a long-lived runner gets no
hooks and the agent reports `online` rather than guessing at what the runner
is doing. See [docker.md](docker.md).

`runnerly ephemeral run` is the exception: it installs them on either
executor, since a one-job runner that cannot tell a finished job from a
crash cannot do its job at all.

## Commands from the control plane

An operator can ask for a restart from the dashboard or the API. The agent
collects it on its next heartbeat, stops the runner with SIGTERM so a job in
flight can finish, and lets the supervisor start it again.

A requested restart is not a failure: it does not count against the backoff
and does not wait one out. The agent reports the outcome, so a command never
sits unresolved, and a command it does not understand comes back as failed
rather than being ignored.

## Not implemented

- **Autoscaling.** Nothing provisions machines or decides how many runners
  to run. Ephemeral runners themselves work; see
  [ephemeral-runners.md](ephemeral-runners.md).
- **Unattended upgrades.** `runnerly upgrade --runners` brings this machine's
  runners up to the release GitHub expects, but only when you run it. Nothing
  upgrades on a schedule, and no command over the heartbeat carries one — an
  upgrade restarts a runner, and restarting one mid-job throws that job away.
- **Self-upgrade.** The Runnerly binary does not replace itself. `upgrade`
  reports when it is behind and leaves the swap to you.
