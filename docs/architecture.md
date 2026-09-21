# Architecture

## The boundary

GitHub owns workflow orchestration and job scheduling. Runnerly owns the
machines that execute jobs. That line does not move.

```text
GitHub
  │  parses workflows, schedules jobs, assigns them to runners
  ▼
Runnerly control plane
  │  inventory, health, registration, policy, events
  ▼
Runnerly agent (one per machine)
  │  installs, configures, supervises
  ▼
actions/runner  ──  GitHub's official runner, unmodified
  │
  ▼
host or Docker
```

Runnerly does not reimplement the Actions runner protocol, parse workflow
YAML, or execute steps. It wraps [`actions/runner`](https://github.com/actions/runner)
and manages its lifecycle.

## Components

| Component | Binary | State |
| --- | --- | --- |
| CLI | `runnerly` | doctor, config, auth, repo, runner, agent, ephemeral, upgrade, server |
| Runner agent | `runnerly-agent` | supervision, restart backoff, systemd |
| Control plane | `runnerly-server` | enrollment, heartbeats, events, API |
| Dashboard | embedded in `runnerly-server` | overview, runners, events, restart, remove |

The control plane is optional at every layer. The CLI works without one, and
so does the agent: with no `server.url` configured it supervises its runner
and reports through structured logs, exactly as before. Nothing about a single
machine requires a database.

The control plane is optional by design. The CLI must stay useful on a machine
that has never seen a server — `doctor` skips server-dependent checks rather
than failing them.

## Code layout

```text
cmd/runnerly/        CLI entry point; main() only
cmd/runnerly-agent/  the daemon systemd runs
cmd/runnerly-server/ the control plane
internal/cli/        cobra command tree, flag parsing, exit codes
internal/agent/      supervising one runner, and its structured logs
internal/auth/       GitHub credential resolution and storage
internal/config/     configuration schema, loading, validation
internal/controlplane/ the agent's client for the server
internal/docker/     preparing and tidying the Docker environment
internal/jobstate/   what the runner is doing, via GitHub's job hooks
internal/doctor/     machine diagnostics and their rendering
internal/github/     a narrow GitHub REST client
internal/machine/    what this computer is, and how hard it is working
internal/metrics/    the Prometheus registry and text format
internal/ratelimit/  per-client token buckets
internal/runner/     installing and configuring actions/runner
internal/secret/     encrypting credentials the server must read back
internal/server/     the control plane's HTTP API
internal/state/      which runners are installed on this machine
internal/store/      the control plane's PostgreSQL layer, and the schema
internal/supervisor/ keeping a child process alive
internal/ui/         terminal output: symbols, color, tables, indentation
internal/version/    build information, injected via -ldflags
internal/web/        serving the embedded dashboard
web/                 the dashboard itself: React, TypeScript, Tailwind
deploy/systemd/      a reference service unit
docs/                this documentation
```

`internal/supervisor` knows nothing about GitHub or runners — it supervises a
command. `internal/agent` is the layer that knows one particular command is a
GitHub runner. Keeping them apart is what lets the restart logic be tested
against fake processes and, separately, against real ones.

Rules that keep the layers honest:

- `internal/cli` holds no logic worth testing on its own. Behavior lives in a
  package that can be tested without a command.
- `internal/ui` knows nothing about runners, GitHub or configuration. It
  renders; it does not decide.
- Packages that touch the machine or the network take their dependencies as a
  struct of function fields (see `doctor.Env`) so tests substitute fakes
  instead of mutating the machine.
- `internal/config` and `internal/github` are leaves: neither imports anything
  else from the project, so neither can be pulled into a dependency cycle by a
  later feature.
- `internal/github` is not a general-purpose GitHub library. It covers the six
  endpoints Runnerly needs, so every error it returns can say something useful
  about Runnerly's situation rather than repeating GitHub's message.

## Why the doctor package looks the way it does

`doctor.Run` takes an `Options` containing an `Env`, and returns a `Report`
value. It performs no output and reads no globals. That makes the whole suite —
missing Docker, dead daemon, unreachable API, invalid config — testable with
fakes, and it is why `doctor` is the model for later components that shell out
or make HTTP calls.

Each `Check` carries a `Detail` (what was found) and a `Remedy` (what to type).
A failing check without a remedy is a bug.

## Configuration and state are separate files

`config.yaml` is what the operator asked for. `runners.yaml` is what Runnerly
did: which runners exist here, where their files are, and which repository each
belongs to.

They are separate so that installing a runner never rewrites a hand-edited,
commented configuration file, and so the agent has something to read that
answers "what am I supervising?" without inferring it. It is also what lets
`runner list`, `runner remove` and the agent work without `--repo` on a machine
with one runner.

## Why the metrics registry is hand-written

The plan asks for five counters and gauges and says not to build an
observability stack. Prometheus' text format is a few lines per metric; its
client library would be the largest dependency in the project.

The trade is real and named in the code: no histograms, so no request
duration percentiles from Prometheus. Those are in the structured logs. If
percentiles become the thing someone needs, the library earns its place then.

## Why a retired runner is not an offline one

An ephemeral runner finishes and is taken apart. Seen only through
heartbeats, that is indistinguishable from a machine falling over, and a
dashboard fills with rows that look like failures but are successes.

So teardown reports the runner retired, and `retired` is a status of its own.
The row is kept rather than deleted: what a runner did is worth more than the
row costs, and its events reference it.

## Why cleanup diffs snapshots instead of pruning

`docker system prune` would be one line. It would also delete a stopped
container belonging to something else on the machine, and a self-hosted
runner is frequently not the only thing on its host.

So the Docker executor records what exists when a job starts and removes what
appeared by the time it finishes. It costs three `docker ls` calls per job and
cannot take anything that was not the job's.

## Why the job hooks earn their place

GitHub's runner will call a script before and after each job. That is the only
supported way to learn, from outside, that a job has started — and it gave
Runnerly two things at once: a moment to clean up, and an honest answer for
`busy`, which until then the agent had been unable to distinguish from
`online`.

The hooks are one-line shell scripts that hand over to the Runnerly binary,
so the logic is Go that can be tested rather than shell that cannot. They
always exit 0, because a hook that fails takes the job with it.

## Why commands travel on the heartbeat

The control plane cannot reach an agent. Runner machines sit behind NAT and
firewalls, and requiring an inbound port on every one of them would be a
worse trade than a short delay. So `restart` is queued and handed over on the
next heartbeat, and every surface that exposes it — the API response, the
dashboard's confirmation — says so rather than implying it was instant.

That also bounds the blast radius of a command an agent does not understand:
it reports the command back as failed instead of ignoring it, so a newer
server against an older agent produces a visible failure rather than silence.

## Why the dashboard is embedded

`runnerly-server` serves the built assets from its own binary, so a
deployment is one process with no static host to configure.

The Go build does not depend on Node. When the dashboard has not been built,
`internal/web/dist` holds only a placeholder and the server serves a page
saying so; the API is unaffected. That keeps `go build ./...` working for
anyone touching the backend, and keeps the UI out of the critical path for a
server that is only ever hit by agents.

`web/` carries its own `go.mod`. It contains no Go code, but npm packages
occasionally vendor some, and without a module boundary `go build ./...`
would compile a dependency's Go source as part of Runnerly.

## Why the agent declares its own wire types

`internal/controlplane` does not import `internal/server`, even though they
describe the same requests. Sharing the types would drag a PostgreSQL driver
into `runnerly-agent`, which has no business carrying one.

The cost of duplication is drift, so it is paid for with a contract test:
`internal/controlplane/contract_test.go` starts a real server against a real
database and drives it with the real client. That catches a mismatch the way
a shared struct would, without the coupling.

## Why hashes for some credentials and encryption for others

Enrollment tokens, machine tokens and session cookies are **hashed**. The
server only needs to recognize one, never read it back, and a hash cannot be
turned into a working credential by someone with a database dump.

A signed-in user's GitHub token is **encrypted**, because the server may have
to present it to GitHub. That needs a key, which is why the server has one and
the CLI does not — and why the CLI stores its own token in a 0600 file and
says so plainly rather than pretending otherwise.

## Why the GitHub client is hand-written

Three direct dependencies is the bar (see below), and Runnerly uses six
GitHub endpoints. A full client library would add a large dependency to save a few
hundred lines, and would hand back GitHub's own error messages — which are
rarely actionable. A 404 from a runner endpoint almost always means "your token
cannot see this", and saying that is worth more than forwarding "Not Found".

## Not built

**Unattended upgrades.** `runnerly upgrade` reports what is out of date and,
with `--runners`, applies it. What is missing is doing that without being
asked. The command channel could carry it, and the reason it does not is not
plumbing: an upgrade restarts a runner, restarting one mid-job throws that
job away, and nothing here knows when a machine is safe to interrupt. That
needs a maintenance window as a first-class idea, not a flag.

**Self-replacing binary.** `upgrade` says when Runnerly itself is behind and
stops there. There are no published releases to test a swap against, and a
binary that rewrites itself badly is worse than one that does not try.

**VM-per-job isolation.** The honest answer when a job needs a real boundary,
and the one thing an ephemeral runner does not give you. See
[security.md](security.md).

Deliberately out of scope: Kubernetes, autoscaling, cloud provisioning, GPU
scheduling, multi-tenancy and local workflow execution.

Windows is not supported yet, which used to be a decision and is now a gap:
the job hooks are a `.cmd` there, the release unpacks from its zip, a
graceful stop is a console event, and CI checks each of those on a real
Windows machine. What is missing is an installer and an end-to-end run. See
[windows.md](windows.md).
macOS is supported: `agent launchd` writes a LaunchAgent, which is what a
Mac has instead of systemd. It is an agent rather than a daemon on purpose,
because a daemon has no login session and a Mac build usually needs one.
See [macos.md](macos.md).

## Dependency policy

Three direct Go dependencies: `spf13/cobra` for the command tree,
`gopkg.in/yaml.v3` for configuration, and `jackc/pgx` for PostgreSQL. Terminal
color, terminal detection, HTTP routing, migrations, UUIDs and cryptography
are handled with the standard library.

The dashboard's runtime dependencies are React, `react-router-dom`, Radix
primitives, `sonner`, `lucide-react`, `class-variance-authority`, `cn` and
the two Geist fonts. Styling is Tailwind.

The interface is built with [shadcn/ui](https://ui.shadcn.com), which is not
a component library in the usual sense: its CLI copies component source into
`web/src/components/ui/`, where it is ours to read and fix. What that pulls
in as an actual dependency is Radix — the behaviour under a dropdown, a
confirmation dialog or a toggle group. That behaviour is focus management,
`aria-*` wiring, Escape and click-outside, arrow-key roving and returning
focus afterwards, and it is worth more than the bytes: every one of those is
something a hand-rolled version gets subtly wrong and nobody notices until
someone navigates by keyboard.

The dashboard did without it at first, on the argument that tables, badges
and two dialogs do not need headless primitives. That was true of what was
on the page then.

`pgx` was added because speaking the PostgreSQL wire protocol is not something
to hand-write. Migrations were not: a forward-only runner over embedded SQL is
about eighty lines, and Go's own `ServeMux` matches methods and path
parameters, so neither a migration library nor a router earned a place.

Adding a dependency requires an answer to: why do we need it, why can't the
standard library do it, is it maintained, is its license compatible?
