# Contributing to Runnerly

Thanks for taking a look. Runnerly is early, so the most useful contributions
right now are small and concrete.

## Before you start

- For anything beyond a bug fix, open an issue first. It is cheaper to agree on
  the shape of a change than to review a large one that went the wrong way.
- Check [docs/architecture.md](docs/architecture.md) so a change lands in the
  right layer.

## Setting up

Requires Go 1.25 or newer.

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make build
make check
```

`make check` runs everything CI runs: formatting, `go vet`, `golangci-lint`,
and the tests. Run it before opening a pull request.

See [docs/development.md](docs/development.md) for the layout of the codebase.

## What we look for

The project prefers code that is **simple, explicit, typed, testable and
boring** over code that is clever or generic.

- **Tests.** New behavior comes with tests. Anything that touches the machine
  or the network goes behind an injectable interface so it can be faked, the
  way `doctor.Env` does.
- **Error messages.** Every user-facing error says what happened, why, and what
  to do next. A failing `doctor` check must suggest an action.
- **Dependencies.** Adding one needs an answer to: why do we need this, why
  can't the standard library do it, is it maintained, is its license
  compatible? Runnerly currently depends on `cobra` and `yaml.v3`, and that is
  the bar.
- **Documentation.** If behavior visible to a user changes, update `docs/` and
  the README in the same pull request.
- **Scope.** One change per pull request. Unrelated refactors make review
  harder.

## Commits and pull requests

Commit messages use the imperative mood and explain why, not just what:

```text
doctor: skip the daemon probe when the Docker CLI is missing

Probing a daemon we cannot reach produced a confusing second failure
for the same root cause.
```

In the pull request, describe what changed, how you verified it, and anything
you deliberately left out.

## Reporting bugs

Include the output of `runnerly version` and, where relevant,
`runnerly doctor --json`. Redact anything sensitive first.

Security issues go to [SECURITY.md](SECURITY.md), not the issue tracker.

## Code of conduct

Participation is covered by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
