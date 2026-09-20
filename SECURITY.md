# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through GitHub's
[security advisory form](https://github.com/bablilayoub/runnerly/security/advisories/new),
or by email to `ayoub@abablil.me`.

Include what you can:

- what the issue is and how it can be triggered
- the Runnerly version (`runnerly version`)
- the operating system and executor mode
- anything that helps reproduce it

You will get an acknowledgement within five working days. Runnerly is an
early-stage project maintained in spare time, so please allow reasonable time
for a fix before disclosing publicly.

## Supported versions

Runnerly has not had a stable release yet. Only `main` is supported.

## Before you deploy

Runnerly manages machines that execute arbitrary code from your GitHub
workflows. That is what a self-hosted runner does, and it is the single largest
risk in running one.

Read [docs/security.md](docs/security.md) before exposing a runner to a public
repository or to pull requests from forks. The short version:

- Prefer private repositories.
- Do not accept workflows from forks on a self-hosted runner.
- Docker is isolation, not a security boundary equivalent to a VM.
- Keep credentials unrelated to CI off runner machines.
- Prefer ephemeral runners when you need a clean environment per job.
