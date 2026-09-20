# Runnerly

## Product definition

Build **Runnerly**, an open-source self-hosted GitHub Actions runner platform.

Tagline:

> Run GitHub Actions on your own infrastructure.

Runnerly should make self-hosted runners dramatically easier to deploy, manage, monitor, isolate, and scale.

This is **not** a replacement for GitHub Actions.

GitHub remains responsible for workflow orchestration and job scheduling. Runnerly manages the infrastructure that executes those jobs.

Use GitHub's official Actions Runner rather than reimplementing the GitHub Actions runner protocol.

Official runner:
https://github.com/actions/runner

Runnerly should sit around it:

```text
                    GitHub
                      │
                Actions workflow
                      │
                job assignment
                      │
                      ▼
               ┌─────────────┐
               │ Runnerly    │
               │             │
               │ Control     │
               │ Plane       │
               └──────┬──────┘
                      │
              runner registration
                      │
        ┌─────────────┼─────────────┐
        ▼             ▼             ▼
    Runner 01      Runner 02      Runner 03
        │             │             │
        ▼             ▼             ▼
      Docker        Docker        Docker
        │             │             │
        ▼             ▼             ▼
      GitHub        GitHub        GitHub
       Jobs          Jobs          Jobs
```

---

# 1. Core product philosophy

Runnerly should feel like:

```bash
curl -fsSL https://runnerly.dev/install.sh | sh
runnerly setup
```

and then:

```text
✓ GitHub connected
✓ Repository configured
✓ Runner registered
✓ Docker detected
✓ Runner online

https://github.com/owner/repo

Runner:
  runnerly-01
  linux / x64
  8 CPU
  16 GB RAM
```

The user should not need to manually:

* download the GitHub runner
* find registration tokens
* configure systemd
* configure Docker
* manage runner labels
* clean stale runners
* inspect runner logs manually
* figure out why a runner disappeared
* manually update runner binaries
* manually create ephemeral runners

Runnerly should automate those tasks.

---

# 2. What NOT to build in v1

Do not attempt to:

* reimplement the GitHub Actions execution engine
* parse and execute arbitrary GitHub Actions YAML ourselves
* become a GitHub Actions replacement
* build our own workflow language
* implement Kubernetes first
* build a cloud-hosted SaaS
* implement Windows/macOS runners initially
* implement GPU scheduling initially
* build a complex distributed scheduler

Do not build an `act` clone.

A future `runnerly local` feature can execute workflows locally, but that is a separate project/scope and should not block the main product.

The initial product is:

> **A better way to run and manage self-hosted GitHub Actions runners.**

---

# 3. Target users

Primary:

* solo developers
* startups
* small engineering teams
* self-hosting enthusiasts
* companies with private infrastructure
* people running CI on VPS/bare metal
* developers who want to avoid GitHub-hosted runner costs
* developers who need custom hardware or private-network access

Initial supported environment:

```text
Linux
Docker
x86_64
ARM64
GitHub.com
```

Prioritize Ubuntu/Debian first.

---

# 4. Main architecture

Use a monorepo.

Recommended stack:

```text
apps/
  web/                 React + Vite + Tailwind
  api/                 Go HTTP API
  cli/                 Go CLI

services/
  runner-agent/        Go
  controller/          Go

packages/
  api-client/
  types/

deploy/
  docker/
  compose/
  systemd/

docs/
```

Actually, simplify where possible.

A good initial structure:

```text
runnerly/
├── cmd/
│   ├── runnerly/
│   ├── runnerly-agent/
│   └── runnerly-server/
│
├── internal/
│   ├── github/
│   ├── runner/
│   ├── docker/
│   ├── agent/
│   ├── server/
│   ├── auth/
│   ├── telemetry/
│   └── config/
│
├── web/
│   └── ...
│
├── migrations/
├── deploy/
├── docs/
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
└── README.md
```

Use Go for the backend, CLI, and runner agent.

Use React + TypeScript + Tailwind + shadcn/ui for the dashboard.

Use PostgreSQL.

Do not introduce Redis or Kafka until there is a concrete need.

---

# 5. Components

## A. Runnerly CLI

Binary:

```bash
runnerly
```

Commands:

```bash
runnerly setup
runnerly login
runnerly doctor

runnerly runner list
runnerly runner create
runnerly runner remove
runnerly runner status

runnerly server install
runnerly server start

runnerly config
runnerly version
```

CLI UX should be excellent.

Examples:

```bash
runnerly setup
```

Interactive setup:

```text
Runnerly Setup

GitHub
  ○ GitHub.com
  ○ GitHub Enterprise

Scope
  ○ Repository
  ○ Organization

Repository:
  bablilayoub/example

Runner name:
  runnerly-01

Execution:
  ○ Docker
  ○ Host

✓ GitHub authenticated
✓ Registration token acquired
✓ Runner installed
✓ Runner configured
✓ Runner started

Runner is online.
```

Also support completely non-interactive usage:

```bash
runnerly setup \
  --repo bablilayoub/example \
  --token "$GITHUB_TOKEN" \
  --name runnerly-01 \
  --executor docker
```

---

# 6. Runner agent

The runner agent is the most important component.

Binary:

```bash
runnerly-agent
```

Its responsibilities:

1. Register with Runnerly server.
2. Authenticate with the control plane.
3. Acquire/configure a GitHub Actions runner.
4. Start the official GitHub Actions runner.
5. Monitor runner health.
6. Report telemetry.
7. Report runner status.
8. Restart failed processes.
9. Handle upgrades.
10. Support ephemeral lifecycle.
11. Optionally execute jobs in isolated Docker environments.

Do not duplicate GitHub's runner logic.

The agent should manage the lifecycle of the official runner.

Conceptually:

```text
runnerly-agent
       │
       ├── GitHub API
       │
       ├── official actions/runner
       │
       ├── Docker
       │
       └── Runnerly API
```

---

# 7. Runner registration

Runnerly should make GitHub runner registration one command.

Example:

```bash
runnerly runner create \
  --repo owner/repo
```

The server/CLI should:

1. authenticate with GitHub
2. request the appropriate runner registration token
3. download the matching runner version
4. verify the downloaded artifact
5. configure it
6. assign labels
7. start it
8. register the machine with Runnerly

The user should never need to manually run:

```bash
./config.sh
./run.sh
```

Runnerly owns this lifecycle.

GitHub supports repository, organization, and enterprise-level runners, as well as custom labels and runner groups. Support repository-level runners first and organization-level runners second.

---

# 8. Labels

Expose a simple label configuration:

```yaml
runner:
  labels:
    - linux
    - x64
    - docker
    - runnerly
```

CLI:

```bash
runnerly runner create \
  --labels linux,x64,docker
```

Dashboard:

```text
Runner
  runnerly-01

Labels
  linux
  x64
  docker
  node
```

Do not create a competing label/routing system.

Runnerly should map directly to GitHub's runner labels/groups.

---

# 9. Execution modes

Implement these modes in order.

## Mode 1 — Host

The official GitHub runner executes on the host.

```text
GitHub
   ↓
Runnerly agent
   ↓
GitHub runner
   ↓
host
```

This is easiest and should be the first functional mode.

## Mode 2 — Docker

The machine runs the runner while workloads are isolated using Docker where GitHub Actions supports containerized execution.

Provide configuration:

```yaml
executor:
  type: docker
```

Do not promise perfect isolation.

Make security boundaries explicit in the documentation.

## Mode 3 — Ephemeral

Support one-job runners.

Lifecycle:

```text
create
  ↓
register
  ↓
online
  ↓
receive one job
  ↓
execute
  ↓
shutdown
  ↓
cleanup
  ↓
deregister
  ↓
destroy
```

GitHub explicitly recommends ephemeral runners for autoscaling scenarios and notes that they can help provide a clean environment for each job.

---

# 10. Control plane

Create a lightweight server.

Binary:

```bash
runnerly-server
```

Responsibilities:

* authentication
* runner registration
* runner inventory
* runner health
* runner metadata
* organization/repository mapping
* labels
* runner events
* configuration
* audit logs
* API
* dashboard backend

Important:

The control plane does NOT replace GitHub's Actions scheduler.

GitHub continues assigning workflow jobs to runners.

The Runnerly control plane manages the machines around those runners.

---

# 11. Data model

PostgreSQL tables:

```text
users
organizations
github_connections
repositories
runners
runner_labels
runner_events
runner_heartbeats
runner_installations
audit_logs
```

Initial `runners` schema:

```text
id
name
status
github_runner_id
github_scope
github_scope_id
repository
organization
os
architecture
cpu_count
memory_bytes
disk_bytes
labels
version
agent_version
last_heartbeat
created_at
updated_at
```

Runner status:

```text
offline
starting
online
busy
stopping
error
```

Do not over-normalize the database in v1.

---

# 12. API

Use REST first.

Example:

```http
POST /api/v1/auth/github
GET  /api/v1/runners
POST /api/v1/runners
GET  /api/v1/runners/:id
DELETE /api/v1/runners/:id

POST /api/v1/runners/:id/restart
POST /api/v1/runners/:id/upgrade

GET /api/v1/events
GET /api/v1/health
```

Agent endpoints:

```http
POST /api/v1/agent/register
POST /api/v1/agent/heartbeat
POST /api/v1/agent/events
GET  /api/v1/agent/config
```

Use authenticated machine tokens for agents.

Never send permanent GitHub credentials to the runner agent when a short-lived token or server-mediated operation is possible.

GitHub's own runner architecture uses time-limited registration/job credentials and stores a runner key pair for authentication. Follow similar principles rather than creating long-lived credential flows.

---

# 13. Authentication

GitHub OAuth for dashboard login.

For personal/self-hosted setup:

```text
GitHub OAuth
     ↓
Runnerly account
     ↓
Connect GitHub organization/repositories
```

For agents:

```text
Runnerly server
      ↓
machine enrollment token
      ↓
agent
      ↓
rotating authentication credential
```

Never store GitHub personal access tokens in plaintext.

Encrypt secrets at rest.

---

# 14. Dashboard

Build a clean monochrome UI.

Avoid enterprise-dashboard clutter.

Main navigation:

```text
Overview
Runners
Repositories
Events
Settings
```

## Overview

```text
Runnerly

Runners
  8 online
  2 busy
  1 offline

Jobs
  14 running
  183 completed

System
  CPU
  Memory
  Disk
```

## Runners

```text
┌───────────────────────────────────────────────┐
│ Runner              Status     CPU    Memory │
│                                               │
│ runnerly-01         ● Online   8      16 GB  │
│ runnerly-02         ● Busy     16     32 GB  │
│ runnerly-03         ● Online   8      32 GB  │
│ runnerly-04         ○ Offline  8      16 GB  │
└───────────────────────────────────────────────┘
```

Clicking a runner:

```text
runnerly-02

Status
Online / Busy

Repository
owner/repo

Labels
linux
x64
docker

Hardware
8 CPU
32 GB RAM
420 GB disk

Agent
0.1.4

Runner
2.x.x

Last heartbeat
3 seconds ago

[Restart] [Remove]
```

---

# 15. Logs

Do not initially try to become a complete centralized logging platform.

For each runner expose:

```text
Agent logs
Runner logs
Recent events
```

Allow:

```text
Download logs
Copy
Search
```

For ephemeral runners, ensure logs survive runner destruction.

GitHub specifically warns that ephemeral runner logs need to be forwarded/preserved externally for troubleshooting.

---

# 16. Health monitoring

The agent should periodically send:

```json
{
  "status": "online",
  "cpu": 31.2,
  "memory": 0.42,
  "disk": 0.67,
  "runner_status": "idle",
  "agent_version": "0.1.0"
}
```

Heartbeat interval:

```text
10-30 seconds
```

Server should mark runners offline after a configurable timeout.

Example:

```text
last heartbeat < 30 sec
→ healthy

30-90 sec
→ stale

> 90 sec
→ offline
```

Make these thresholds configurable.

---

# 17. Automatic recovery

If the official runner crashes:

```text
runner process exited
        ↓
Runnerly detects failure
        ↓
attempt restart
        ↓
if repeated failures
        ↓
mark runner unhealthy
        ↓
show reason in dashboard
```

Implement exponential backoff.

Example:

```text
5s
10s
20s
40s
80s
```

Then stop retrying after a reasonable threshold.

---

# 18. Updates

Runnerly should manage:

```text
Runnerly agent version
GitHub runner version
```

Show:

```text
Updates available

Runnerly Agent
0.1.2 → 0.1.3

GitHub Runner
2.x.x → 2.x.x

[Update]
```

Support:

```bash
runnerly upgrade
```

Do not automatically perform disruptive upgrades without an explicit policy.

Support:

```yaml
updates:
  auto: false
```

and eventually:

```yaml
updates:
  auto: true
  maintenance_window: "02:00-04:00"
```

GitHub's self-hosted runner can automatically update by default; Runnerly should make update behavior visible and controllable rather than hiding it.

---

# 19. Security model

This is a critical part of the project.

Create:

```text
SECURITY.md
docs/security.md
```

Document clearly:

* self-hosted runners execute arbitrary workflow code
* private repositories should be the default recommendation
* public repositories and fork pull requests can be dangerous
* never expose host credentials unnecessarily
* Docker isolation is not equivalent to a secure VM boundary
* runner machines should not contain sensitive credentials unrelated to CI
* use ephemeral runners for higher isolation
* network access should be restricted where practical

GitHub explicitly warns about workflows from forks executing code on self-hosted runners.

Create a first-class configuration:

```yaml
security:
  allow_public_repositories: false
  allow_fork_workflows: false
```

Even if those policies are initially enforced operationally/documentationally rather than perfectly enforced by Runnerly.

---

# 20. Docker support

The installation experience should support:

```bash
docker compose up -d
```

Architecture:

```text
docker compose

runnerly-server
postgres
```

Runner machines can run:

```text
runnerly-agent
github actions runner
docker
```

Provide:

```text
deploy/docker-compose.yml
deploy/systemd/
```

Also provide an official Docker image for:

```text
runnerly-server
runnerly-agent
```

---

# 21. `runnerly doctor`

This should be one of the best CLI commands.

```bash
runnerly doctor
```

Output:

```text
Runnerly Doctor

✓ Linux detected
✓ x86_64 detected
✓ Docker installed
✓ Docker daemon running
✓ Git installed
✓ curl installed
✓ outbound HTTPS available
✓ GitHub API reachable
✓ Runnerly server reachable
✓ runner credentials valid

Everything looks good.
```

Failure example:

```text
✗ Docker daemon unavailable

Docker is installed but the daemon is not running.

Try:
  sudo systemctl start docker
```

Make error messages extremely actionable.

---

# 22. Installation

Linux:

```bash
curl -fsSL https://runnerly.dev/install.sh | sh
```

Also:

```bash
brew install runnerly
```

later.

The installer should:

1. detect OS
2. detect architecture
3. download correct binary
4. verify checksum
5. install to `/usr/local/bin/runnerly`
6. optionally install systemd service
7. run `runnerly doctor`

Never silently execute privileged operations.

When root privileges are required, explain why.

---

# 23. systemd

Provide:

```text
runnerly-agent.service
```

The agent should run as a dedicated non-root user whenever possible.

Example:

```text
User=runnerly
Group=runnerly
```

Avoid:

```text
User=root
```

unless a specific execution feature requires it.

---

# 24. GitHub integration

Use GitHub APIs to automate runner registration and management.

Support:

```text
repository runners
organization runners
```

later:

```text
enterprise runners
runner groups
```

Implement:

```text
create registration token
remove runner
list runners
inspect runner
labels
```

Do not hard-code assumptions about GitHub's current runner version.

Fetch the supported/current runner release information dynamically where appropriate.

---

# 25. CLI UX principles

The CLI should feel closer to:

```text
Railway
Vercel
Fly
Tailscale
```

than to:

```text
enterprise infrastructure software
```

Commands should be predictable:

```bash
runnerly setup
runnerly status
runnerly doctor
runnerly runner list
runnerly runner create
runnerly runner remove
runnerly logs
```

Use colors only where useful.

Tables should look good.

Errors should explain:

```text
what happened
why
what to do next
```

---

# 26. MVP milestones

## Milestone 0 — Repository

Create:

```text
README.md
LICENSE
SECURITY.md
CONTRIBUTING.md
CODE_OF_CONDUCT.md
```

License:

```text
MIT
```

Set up:

```text
Go
React
TypeScript
Postgres
Docker
GitHub Actions
golangci-lint
Prettier
ESLint
```

CI should test Runnerly itself.

---

## Milestone 1 — CLI

Implement:

```bash
runnerly version
runnerly doctor
runnerly setup
```

`doctor` should work without the server.

---

## Milestone 2 — GitHub integration

Implement:

```text
OAuth/PAT authentication
repository discovery
runner registration
runner removal
runner listing
runner labels
```

Acceptance test:

```bash
runnerly runner create --repo owner/repo
```

results in a visible self-hosted runner in GitHub.

---

## Milestone 3 — Agent

Implement:

```text
agent enrollment
heartbeat
runner lifecycle
runner process monitoring
automatic restart
systemd
```

Acceptance test:

```text
server sees runner as Online
```

and:

```text
kill runner process
```

causes the agent to detect and recover.

---

## Milestone 4 — Dashboard

Implement:

```text
login
overview
runner list
runner detail
runner events
runner restart
runner remove
```

Keep the UI minimalist.

---

## Milestone 5 — Docker execution

Add:

```text
Docker executor
```

Provide a clean configuration file.

Test with real GitHub workflows:

```yaml
name: test

on:
  push:

jobs:
  test:
    runs-on: [self-hosted, linux, x64]

    steps:
      - uses: actions/checkout@v4
      - run: npm ci
      - run: npm test
```

---

## Milestone 6 — Ephemeral runners

Implement:

```text
one job
cleanup
deregister
destroy
```

Add lifecycle visibility in the dashboard.

---

## Milestone 7 — Production hardening

Add:

```text
TLS
secret encryption
machine token rotation
audit logs
health checks
rate limiting
input validation
structured logging
metrics
backup instructions
upgrade mechanism
```

---

# 27. Testing strategy

Do not rely only on unit tests.

Create:

```text
unit tests
integration tests
end-to-end tests
```

End-to-end environment:

```text
Runnerly server
     ↓
Postgres
     ↓
Runner agent
     ↓
GitHub test repository
     ↓
real Actions workflow
```

Test:

```text
runner registration
workflow execution
runner restart
agent restart
runner disconnect
runner deletion
invalid token
expired token
upgrade
ephemeral lifecycle
```

Create test fixtures for GitHub API responses.

Avoid making CI depend entirely on GitHub's live API.

---

# 28. Observability

Use structured JSON logs internally.

Every event should contain:

```text
timestamp
component
runner_id
event
severity
message
```

Example:

```json
{
  "level": "info",
  "component": "runner-agent",
  "runner_id": "r_123",
  "event": "runner_online"
}
```

Expose basic Prometheus metrics later:

```text
runnerly_runners_total
runnerly_runners_online
runnerly_runner_heartbeats_total
runnerly_runner_restarts_total
runnerly_agent_errors_total
```

Do not build a complex observability stack.

---

# 29. Documentation

Documentation must be written as the product is built.

Required:

```text
docs/
├── getting-started.md
├── installation.md
├── github.md
├── runners.md
├── docker.md
├── ephemeral-runners.md
├── security.md
├── configuration.md
├── troubleshooting.md
├── architecture.md
└── development.md
```

The README should allow a developer to understand the product within 30 seconds.

Suggested structure:

```text
Runnerly

Run GitHub Actions on your own infrastructure.

$ curl ...
$ runnerly setup

✓ Runner online

Features
- One-command setup
- Self-hosted
- Docker
- Ephemeral runners
- Runner monitoring
- Automatic recovery
- CLI
- Web dashboard
```

---

# 30. Design language

Runnerly should visually fit with OpenHole and Nixploy.

Use:

```text
monochrome
minimal
technical
high contrast
dense but readable
no gradients
no giant SaaS illustrations
```

Think:

```text
terminal
GitHub
Vercel
Linear
Tailscale
```

but with an infrastructure aesthetic.

---

# 31. Future roadmap

Do NOT build these initially, but architect so they are possible.

### Runner pools

```text
pool: default
pool: gpu
pool: arm64
pool: high-memory
```

### Autoscaling

```text
pending jobs
     ↓
Runnerly
     ↓
provision VM
     ↓
ephemeral runner
     ↓
job
     ↓
destroy VM
```

### Cloud providers

Potential providers:

```text
Hetzner
AWS
GCP
Azure
DigitalOcean
Proxmox
Libvirt
```

### VM isolation

Potential backends:

```text
Firecracker
KVM
LXC
Cloud VMs
```

This becomes much more interesting than plain Docker.

### GitHub Enterprise

Support GitHub Enterprise Server later.

### Multi-region runner pools

```text
Paris
Frankfurt
Virginia
Singapore
```

### Cost visibility

Eventually:

```text
Runner cost
CPU hours
RAM hours
storage
```

---

# 32. Long-term product vision

The end state should look like:

```text
                      GitHub
                         │
                         ▼
                  ┌─────────────┐
                  │ Runnerly    │
                  │ Control     │
                  │ Plane       │
                  └──────┬──────┘
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
       Pool A          Pool B          Pool C

      Standard          GPU            ARM

          │              │              │
          ▼              ▼              ▼

       ephemeral       ephemeral       ephemeral
       runner          runner          runner
          │              │              │
          ▼              ▼              ▼
        Docker          Docker         Docker
```

Eventually Runnerly should be able to turn:

```text
"Here is a VPS."
```

into:

```text
"A production-grade GitHub Actions runner fleet."
```

with minimal configuration.

---

# 33. First release target

The first public release should support this complete flow:

```bash
curl -fsSL https://runnerly.dev/install.sh | sh

runnerly setup
```

User authenticates with GitHub.

Then:

```text
✓ Connected to GitHub
✓ Repository selected
✓ Runner created
✓ Runner registered
✓ Runner online
```

They push:

```yaml
jobs:
  test:
    runs-on: self-hosted
```

GitHub assigns the job.

Runnerly runner executes it.

Dashboard shows:

```text
runnerly-01
● Online

Last job:
test
success
2m 14s
```

That is the **definition of done for v1**.

Do not move to autoscaling, Kubernetes, cloud provisioning, GPU support, or local workflow execution until this path is rock-solid.

---

# 34. Development instructions for Claude Code

Work incrementally.

Before implementing a feature:

1. Inspect the repository.
2. Understand the existing architecture.
3. Create the smallest clean implementation.
4. Add tests.
5. Run formatting/linting.
6. Run unit tests.
7. Run integration tests when relevant.
8. Update documentation.
9. Update the README when user-facing behavior changes.

Do not generate enormous amounts of code at once.

Do not create abstractions without a concrete use case.

Prefer:

```text
simple
explicit
typed
testable
boring
```

over:

```text
clever
generic
over-engineered
```

Do not add dependencies without justification.

Every new dependency should answer:

```text
Why do we need this?
Why can't the standard library handle it?
Is it maintained?
Is its license compatible?
```

---

# 35. Definition of success

Runnerly succeeds when a developer who currently sees GitHub's:

```text
Settings
→ Actions
→ Runners
→ New self-hosted runner
→ choose OS
→ copy commands
→ install
→ configure
→ create service
→ debug
```

can instead do:

```bash
runnerly setup
```

and have a production-usable runner running in minutes.

The product should make self-hosted GitHub Actions feel like a normal developer tool rather than an infrastructure chore.
