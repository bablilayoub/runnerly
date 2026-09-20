# Security model

Read this before running a self-hosted runner. The risks below come from
self-hosted runners in general, not from Runnerly specifically — Runnerly
just makes them easier to create.

## The core risk

**A self-hosted runner executes arbitrary code.** Any workflow that can run on
your runner can run any command on that machine, as the user the runner runs
as, with that machine's network access.

This is not a bug in GitHub Actions. It is what a runner is for. The security
question is therefore not "can workflow code run here?" but "whose workflow
code, and what can it reach?".

## Rules that matter most

### 1. Prefer private repositories

Use self-hosted runners with private repositories you control. This is
[GitHub's own recommendation](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/about-self-hosted-runners#self-hosted-runner-security).

### 2. Never accept fork pull requests on a self-hosted runner

If a public repository uses a self-hosted runner, anyone who can open a pull
request can propose a workflow change and get it executed on your hardware.
`pull_request_target` and workflows that check out PR code are especially
dangerous.

Runnerly's configuration makes the intent explicit:

```yaml
security:
  allow_public_repositories: false
  allow_fork_workflows: false
```

These are **advisory today**. Runnerly records and surfaces the policy; it
does not yet enforce it against GitHub. Enforce it in GitHub's own settings:
require approval for all outside contributors' workflow runs, and restrict
which repositories may use a runner group.

### 3. Docker is isolation, not a security boundary

`executor.type: docker` gives you a cleaner environment and limits accidental
cross-job contamination. It does **not** give you the boundary a virtual
machine does. A container sharing the host kernel, and especially one with
access to the Docker socket, should be treated as having a path to the host.

If you need a real boundary, use a disposable VM per job. VM-backed isolation
is on the roadmap and is not implemented.

### 4. Keep unrelated credentials off runner machines

Do not run runners on machines that hold production SSH keys, cloud
credentials, database passwords, or personal data. A runner machine should be
disposable and should hold only what CI needs.

Specifically:

- do not run the runner as `root` (see below)
- do not mount host secrets into job containers
- do not give the runner machine broad cloud IAM permissions
- assume anything on the machine is readable by any job that runs on it

### 5. Restrict network access

Runners usually need outbound HTTPS to GitHub and to package registries, and
nothing else. If the machine sits inside a private network, restrict what it can
reach. A compromised runner is a foothold inside that network.

### 6. Prefer ephemeral runners

A runner that handles one job and is then destroyed gives every job a clean
environment and limits what a compromised job can leave behind for the next
one. GitHub recommends ephemeral runners for autoscaling.

Ephemeral support is planned and not implemented.

## How Runnerly is built

These are commitments about Runnerly's own behavior.

- **No long-lived GitHub credentials on runner machines.** Registration uses
  GitHub's short-lived registration tokens. Where an operation can be mediated
  by the control plane instead of handing a credential to an agent, it will be.
- **No plaintext personal access tokens.** Secrets are encrypted at rest.
- **The configuration file is written `0600`.**
- **No silent privileged operations.** When something needs root, Runnerly
  says why before doing it.
- **The agent runs as a dedicated non-root user** wherever the execution mode
  allows it.

Some of these describe components that do not exist yet. They are stated here
so the design is fixed before the code is written, not after.

## Reporting a problem

See [SECURITY.md](../SECURITY.md).
