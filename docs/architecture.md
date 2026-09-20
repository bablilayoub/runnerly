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
| Control plane | `runnerly-server` | not implemented |
| Dashboard | web | not implemented |

The agent has no control plane to enroll with or send heartbeats to, so it
reports through structured logs on stdout. That is deliberate sequencing rather
than a gap to paper over: a machine with a working, self-healing runner is
useful on its own, and the enrollment protocol is easier to design once there
is a server to design it against.

The control plane is optional by design. The CLI must stay useful on a machine
that has never seen a server — `doctor` skips server-dependent checks rather
than failing them.

## Code layout

```text
cmd/runnerly/        CLI entry point; main() only
cmd/runnerly-agent/  the daemon systemd runs
internal/cli/        cobra command tree, flag parsing, exit codes
internal/agent/      supervising one runner, and its structured logs
internal/auth/       GitHub credential resolution and storage
internal/config/     configuration schema, loading, validation
internal/doctor/     machine diagnostics and their rendering
internal/github/     a narrow GitHub REST client
internal/runner/     installing and configuring actions/runner
internal/state/      which runners are installed on this machine
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

## Why the GitHub client is hand-written

Two direct dependencies is the bar (see below), and Runnerly uses six GitHub
endpoints. A full client library would add a large dependency to save a few
hundred lines, and would hand back GitHub's own error messages — which are
rarely actionable. A 404 from a runner endpoint almost always means "your token
cannot see this", and saying that is worth more than forwarding "Not Found".

## Planned

Roughly in order:

1. **Control plane** — Postgres-backed inventory, runner events, agent
   endpoints, machine tokens. This is what agent enrollment and heartbeats
   need, and neither can be designed honestly without it.
2. **Dashboard** — React and TypeScript, monochrome, minimal.
3. **Docker executor**, then **ephemeral runners**.

Deliberately out of scope until the above is solid: Kubernetes, autoscaling,
cloud provisioning, GPU scheduling, Windows and macOS runners, and local
workflow execution.

## Dependency policy

Two direct dependencies today: `spf13/cobra` for the command tree and
`gopkg.in/yaml.v3` for configuration. Terminal color, terminal detection and
HTTP are handled with the standard library.

Adding a dependency requires an answer to: why do we need it, why can't the
standard library do it, is it maintained, is its license compatible?
