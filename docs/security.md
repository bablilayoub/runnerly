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

`allow_public_repositories` **is enforced**: `runnerly runner create` looks the
repository up and refuses to register a runner against a public one unless you
set this to true or pass `--allow-public`. Organization runners are not checked,
because an organization's repositories cannot be enumerated cheaply; Runnerly
says so rather than implying a check it did not make.

`allow_fork_workflows` is recorded but **not** enforced — Runnerly cannot
control which workflows GitHub dispatches. Enforce it in GitHub's own settings:
require approval for all outside contributors' workflow runs, and restrict which
repositories may use a runner group.

### 3. Docker is isolation, not a security boundary

`executor.type: docker` gives you a cleaner environment and limits accidental
cross-job contamination. It does **not** give you the boundary a virtual
machine does. A container sharing the host kernel, and especially one with
access to the Docker socket, should be treated as having a path to the host.

Concretely: **anything that can reach the Docker socket has root on the
host**, because it can start a container that mounts `/`. Adding the runner's
user to the `docker` group grants that user root, and a workflow that mounts
the socket into a job container hands the host to whatever runs there.
Rootless Docker is a real improvement; point `executor.docker.host` at it.
See [docker.md](docker.md).

If you need a real boundary, use a disposable VM per job. VM-backed isolation
is not built, and an ephemeral runner is not a substitute: it gives every job
a clean environment, not a boundary around it.

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

```bash
runnerly ephemeral run --repo acme/widgets
```

Each run gets its own registration and its own working directory, and the
directory is deleted afterwards. The logs are kept outside it, so what a run
did survives the runner being taken apart. See
[ephemeral-runners.md](ephemeral-runners.md).

What this does not give you is isolation from the host — see rule 3. A clean
environment per job and a boundary around the job are different things.

## Where your GitHub token is kept

`runnerly login` stores the token in `credentials.yaml` beside `config.yaml`,
with mode 0600 on a directory with mode 0700.

On Windows that is not what happens, and it is worth saying rather than
leaving the sentence above to be read as universal: Go turns a mode into
the read-only attribute and nothing else, so the file is protected by the
ACL it inherits from its directory, which Runnerly does not set. In
practice that directory is inside the user's profile and is already
private — but Runnerly is not the thing making it so. Windows is not a
supported platform yet; this is one of the reasons. See
[windows.md](windows.md).

**That file is not encrypted.** This is a deliberate choice, not an oversight. A
local CLI has nowhere to keep a key that an attacker who can read the file could
not also read, so encrypting the token beside its own key would imply a
guarantee it cannot make. Mode 0600 is the real protection, and saying so is
more useful than obfuscation that looks like security.

If you do not want a token on disk, do not store one:

```bash
export RUNNERLY_GITHUB_TOKEN="$(pass github/runnerly)"
```

An environment token always beats the stored one and is never written.
`runnerly auth status` always says which source is in use, so this is never a
surprise.

Server-side storage is a different problem with a different answer, and the
control plane does encrypt at rest, because it has somewhere to put a key.
A signed-in user's GitHub token is encrypted with AES-256-GCM under
`server.secret_key`; enrollment tokens, machine tokens and session cookies are
hashed instead, since the server only ever has to recognize one. The key is not
in the database, so a dump alone yields nothing. See [operations.md](operations.md).

Prefer a token scoped to exactly what you need — see [github.md](github.md) —
and note that a runner machine does not need your token at all. Registration
happens with a short-lived registration token that Runnerly requests, passes to
`config.sh`, and never writes to disk.

## How Runnerly is built

These are commitments about Runnerly's own behavior.

- **No long-lived GitHub credentials on runner machines.** Registration uses
  GitHub's short-lived registration tokens. An enrolled agent holds a machine
  token for Runnerly's own API and no GitHub credential at all; the server
  rotates that token on a schedule, so a leaked one is worth something for a
  bounded time.
- **Downloads are verified.** The official runner archive is checked against the
  SHA-256 checksum GitHub publishes with it. A mismatch deletes the file rather
  than executing it, and a release GitHub publishes no checksum for is refused.
- **Archives cannot escape their directory.** Every entry unpacked from the
  runner release is resolved against the install directory and rejected if it
  would land outside it, including symlink targets.
- **Registration tokens are redacted** from error output, which tends to end up
  in logs and issue reports.
- **The configuration and credentials files are written `0600`**, on the
  platforms where that means something. Not on Windows; see above.
- **No silent privileged operations.** When something needs root, Runnerly
  says why before doing it.
- **The agent runs as a dedicated non-root user** wherever the execution mode
  allows it. The unit `agent systemd` prints does this; it does not install
  itself, so you read it before root touches anything.
- **Credentials the server keeps are hashed unless it must read them back**,
  and encrypted when it must. Which is which is in
  [architecture.md](architecture.md).

## In production

TLS, rate limiting, credential rotation and the audit trail are covered in
[operations.md](operations.md). The short version:

- Put TLS in front of the control plane, or configure `server.tls`. Machine
  tokens are bearer credentials.
- Leave `trust_forwarded_for` off unless a proxy you control sets the header.
- Set `server.oauth.allowed_logins`, or anyone who completes GitHub sign-in
  gets in.
- Back up the secret key with your other secrets; it is not in the database.

## Reporting a problem

See [SECURITY.md](../SECURITY.md).
