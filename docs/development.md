# Development

## Prerequisites

- Go 1.24 or newer
- `make`
- Docker, if you want the Docker checks in `doctor` to exercise a real daemon

## Everyday commands

```bash
make build      # binary into dist/
make test       # unit tests
make check      # everything CI runs: fmt-check, vet, lint, test
make run ARGS="doctor --offline"
```

`make check` is the gate. If it passes locally, CI should pass.

`make lint` needs no install step. It runs a pinned `golangci-lint` through
`go run`, which fetches it into the module cache the first time (slow once,
cached after). CI runs the same target, so local and CI results agree.

The version lives in one place, `GOLANGCI_LINT_VERSION` in the Makefile. If you
already have the binary installed and want to use it:

```bash
make lint GOLANGCI_LINT=golangci-lint
```

## Layout

See [architecture.md](architecture.md) for the package layout and the rules
about what may import what.

## Testing conventions

**Anything that touches the machine or the network is injected.** The pattern
is a struct of function fields, as in `doctor.Env`:

```go
type Env struct {
    GOOS       string
    GOARCH     string
    LookPath   func(file string) (string, error)
    Run        func(ctx context.Context, name string, args ...string) ([]byte, error)
    HTTPClient *http.Client
    Dial       func(ctx context.Context, network, addr string) (net.Conn, error)
}
```

`DefaultEnv()` wires it to the real machine; tests build one where every
dependency succeeds and then break exactly one thing. A test must never depend
on whether the machine running it happens to have Docker.

Other conventions:

- Table-driven tests where the cases are genuinely parallel; separate named
  tests where they are not.
- Failure messages say what was expected and what happened:
  `t.Errorf("docker daemon = %q, want fail", c.Status)`.
- CLI tests run the real command tree against `bytes.Buffer` streams through
  `NewRootCommand(out, errOut)`. No subprocess, no global state.
- Use `t.TempDir()` and `t.Setenv()`; never write to a real home directory.

## The injection seams

Three packages reach outside the process, and each takes its dependencies as a
struct of function fields so tests never touch the machine or the network:

| Package | Seam | Replaced in tests with |
| --- | --- | --- |
| `internal/doctor` | `doctor.Env` | fake `LookPath`, `Run`, `Dial`, HTTP client |
| `internal/runner` | `runner.Env` | fake `Run`; an `httptest` server for downloads |
| `internal/supervisor` | `supervisor.StartFunc` | a fake `Process` the test drives |
| `internal/agent` | `agent.Options.Start` | the same fake, through the agent |
| `internal/cli` | `env.newGitHubClient`, `env.runnerEnv` | a client pointed at `httptest` |

The supervisor is tested both ways on purpose. Fakes cover the state machine —
backoff, giving up, clearing history, the kill path — deterministically and
fast. A second file, `process_unix_test.go`, runs real shell scripts: it starts
one, kills it from outside, and checks the supervisor brings it back, which is
the only way to know process groups and signals actually work.

The CLI seams are why command tests exercise the real cobra tree: `newEnv` and
`newRootCommand` are separate, so a test builds an environment, swaps the two
fields, and runs actual arguments through actual flag parsing.

```go
opts := []option{withGitHub(t, handler), withRunnerEnv(&commands)}
out, _, err := runCLI(t, opts, "--config", cfg, "runner", "create", "--repo", "acme/widgets")
```

## Adding a GitHub endpoint

`internal/github` is deliberately narrow. When adding a call:

1. Put the request in the file for its resource (`runners.go`, `repos.go`).
2. Go through `c.do`, which sets the API version, handles auth, and turns a
   non-2xx response into an `*Error`. It returns headers, not the response —
   the body is already consumed and closed.
3. Wrap the error with what Runnerly was trying to do:
   `fmt.Errorf("list runners for %s: %w", scope, err)`.
4. If the failure needs explaining beyond GitHub's own message, extend
   `Error.Hint` rather than the call site, so every endpoint benefits.
5. Test it against an `httptest` server, asserting the path and method. Several
   endpoints differ only by path, and that is exactly where a typo hides.

## Adding a doctor check

1. Write `checkThing(...) Check` in `internal/doctor`. Take what you need from
   `Options` and `Env`; do not call the machine directly.
2. Add it to the slice in `Run`. Order is the order the operator reads.
3. Give a failing result a `Remedy` that can be pasted into a shell. A failing
   check without one is a bug.
4. Decide honestly between `StatusFail` and `StatusWarn`. Fail means Runnerly
   cannot work. Warn means it can, but you should know something.
5. Use `StatusSkip` when the check does not apply — a missing control plane or a
   host executor is not a failure.
6. Add a test for each status the check can produce.

## Adding a command

1. Add `newThingCommand(e *env) *cobra.Command` in `internal/cli`.
2. Register it in `NewRootCommand`.
3. Keep the `RunE` body thin: parse flags, call a package, render. Logic worth
   testing belongs in that package.
4. Return an error with what happened, why, and what to do next. To control the
   exit code without printing another line, return `&ExitError{Code: n}` — see
   `doctor`.
5. Write to `e.out` and `e.errOut`, never to `os.Stdout` directly, or the
   command becomes untestable.

## Version injection

`internal/version` holds package-level variables set at link time. `make build`
supplies them from git:

```bash
go build -ldflags "-X github.com/bablilayoub/runnerly/internal/version.Version=0.1.0" ./cmd/runnerly
```

An unstamped build reports `dev`.

## CI

`.github/workflows/ci.yml` runs on every push to `main` and every pull request:

- **test** on Go 1.24 and current stable: formatting, `go mod tidy` cleanliness,
  `go vet`, and the tests with `-race`
- **lint**: `golangci-lint` at the pinned version
- **build**: linux/amd64, linux/arm64 and darwin/arm64, uploading each binary

## Dependencies

Two direct dependencies: `spf13/cobra` and `gopkg.in/yaml.v3`. Adding a third
needs a justification in the pull request — see [CONTRIBUTING.md](../CONTRIBUTING.md).
