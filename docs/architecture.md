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
| CLI | `runnerly` | doctor, config, auth, repo and runner commands implemented |
| Runner agent | `runnerly-agent` | not implemented |
| Control plane | `runnerly-server` | not implemented |
| Dashboard | web | not implemented |

The CLI installs and registers runners today. Keeping them running is the
agent's job and does not exist yet, which is why `runner create` stops after
registration and tells you to start the process yourself.

The control plane is optional by design. The CLI must stay useful on a machine
that has never seen a server — `doctor` skips server-dependent checks rather
than failing them.

## Code layout

```text
cmd/runnerly/        CLI entry point; main() only
internal/cli/        cobra command tree, flag parsing, exit codes
internal/auth/       GitHub credential resolution and storage
internal/config/     configuration schema, loading, validation
internal/doctor/     machine diagnostics and their rendering
internal/github/     a narrow GitHub REST client
internal/runner/     installing and configuring actions/runner
internal/ui/         terminal output: symbols, color, tables, indentation
internal/version/    build information, injected via -ldflags
docs/                this documentation
```

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

## Why the GitHub client is hand-written

Two direct dependencies is the bar (see below), and Runnerly uses six GitHub
endpoints. A full client library would add a large dependency to save a few
hundred lines, and would hand back GitHub's own error messages — which are
rarely actionable. A 404 from a runner endpoint almost always means "your token
cannot see this", and saying that is worth more than forwarding "Not Found".

## Planned

Roughly in order:

1. **Runner agent** — enrollment, heartbeat, runner supervision, restart with
   exponential backoff, systemd unit. This is what turns a registered runner
   into one that stays up.
2. **Control plane** — Postgres-backed inventory, runner events, agent
   endpoints, machine tokens.
3. **Dashboard** — React and TypeScript, monochrome, minimal.
4. **Docker executor**, then **ephemeral runners**.

Deliberately out of scope until the above is solid: Kubernetes, autoscaling,
cloud provisioning, GPU scheduling, Windows and macOS runners, and local
workflow execution.

## Dependency policy

Two direct dependencies today: `spf13/cobra` for the command tree and
`gopkg.in/yaml.v3` for configuration. Terminal color, terminal detection and
HTTP are handled with the standard library.

Adding a dependency requires an answer to: why do we need it, why can't the
standard library do it, is it maintained, is its license compatible?
