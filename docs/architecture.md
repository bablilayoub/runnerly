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
| CLI | `runnerly` | `version`, `doctor`, `config` implemented |
| Runner agent | `runnerly-agent` | not implemented |
| Control plane | `runnerly-server` | not implemented |
| Dashboard | web | not implemented |

The control plane is optional by design. The CLI must stay useful on a machine
that has never seen a server — `doctor` skips server-dependent checks rather
than failing them.

## Code layout

```text
cmd/runnerly/      CLI entry point; main() only
internal/cli/        cobra command tree, flag parsing, exit codes
internal/config/     configuration schema, loading, validation
internal/doctor/     machine diagnostics and their rendering
internal/ui/         terminal output: symbols, color, indentation
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
- `internal/config` is a leaf: it imports nothing from the rest of the project.

## Why the doctor package looks the way it does

`doctor.Run` takes an `Options` containing an `Env`, and returns a `Report`
value. It performs no output and reads no globals. That makes the whole suite —
missing Docker, dead daemon, unreachable API, invalid config — testable with
fakes, and it is why `doctor` is the model for later components that shell out
or make HTTP calls.

Each `Check` carries a `Detail` (what was found) and a `Remedy` (what to type).
A failing check without a remedy is a bug.

## Planned

Roughly in order:

1. **GitHub integration** — authentication, repository discovery, registration
   tokens, runner create/list/remove. The acceptance test is
   `runnerly runner create --repo owner/repo` producing a visible runner in
   GitHub.
2. **Runner agent** — enrollment, heartbeat, runner supervision, restart with
   exponential backoff, systemd unit.
3. **Control plane** — Postgres-backed inventory, runner events, agent
   endpoints, machine tokens.
4. **Dashboard** — React and TypeScript, monochrome, minimal.
5. **Docker executor**, then **ephemeral runners**.

Deliberately out of scope until the above is solid: Kubernetes, autoscaling,
cloud provisioning, GPU scheduling, Windows and macOS runners, and local
workflow execution.

## Dependency policy

Two direct dependencies today: `spf13/cobra` for the command tree and
`gopkg.in/yaml.v3` for configuration. Terminal color, terminal detection and
HTTP are handled with the standard library.

Adding a dependency requires an answer to: why do we need it, why can't the
standard library do it, is it maintained, is its license compatible?
