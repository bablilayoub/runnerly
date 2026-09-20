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
| CLI | `runnerly` | doctor, config, auth, repo, runner and agent commands |
| Runner agent | `runnerly-agent` | supervision, restart backoff, systemd |
| Control plane | `runnerly-server` | enrollment, heartbeats, events, API |
| Dashboard | web | not implemented |

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
internal/doctor/     machine diagnostics and their rendering
internal/github/     a narrow GitHub REST client
internal/runner/     installing and configuring actions/runner
internal/secret/     encrypting credentials the server must read back
internal/server/     the control plane's HTTP API
internal/state/      which runners are installed on this machine
internal/store/      the control plane's PostgreSQL layer, and the schema
internal/supervisor/ keeping a child process alive
internal/ui/         terminal output: symbols, color, tables, indentation
internal/version/    build information, injected via -ldflags
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

Two direct dependencies is the bar (see below), and Runnerly uses six GitHub
endpoints. A full client library would add a large dependency to save a few
hundred lines, and would hand back GitHub's own error messages — which are
rarely actionable. A 404 from a runner endpoint almost always means "your token
cannot see this", and saying that is worth more than forwarding "Not Found".

## Planned

Roughly in order:

1. **Dashboard** — React and TypeScript, monochrome, minimal, on the API that
   now exists. Restart and upgrade commands belong here too: both need a
   command channel from server to agent, worth designing alongside the UI that
   would drive it.
2. **Docker executor**, then **ephemeral runners**.

Deliberately out of scope until the above is solid: Kubernetes, autoscaling,
cloud provisioning, GPU scheduling, Windows and macOS runners, and local
workflow execution.

## Dependency policy

Three direct dependencies: `spf13/cobra` for the command tree,
`gopkg.in/yaml.v3` for configuration, and `jackc/pgx` for PostgreSQL. Terminal
color, terminal detection, HTTP routing, migrations, UUIDs and cryptography
are handled with the standard library.

`pgx` was added because speaking the PostgreSQL wire protocol is not something
to hand-write. Migrations were not: a forward-only runner over embedded SQL is
about eighty lines, and Go's own `ServeMux` matches methods and path
parameters, so neither a migration library nor a router earned a place.

Adding a dependency requires an answer to: why do we need it, why can't the
standard library do it, is it maintained, is its license compatible?
