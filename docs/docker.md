# The Docker executor

```yaml
executor:
  type: docker
```

## What it actually does

It does **not** put your jobs in containers. Whether a job is containerized is
decided by the workflow, using GitHub Actions' own features:

```yaml
jobs:
  test:
    runs-on: [self-hosted, linux, x64]
    container: node:22          # the job runs in here
    services:
      postgres:                 # and so does this
        image: postgres:16
```

The runner starts those containers. Runnerly's job is the environment around
them: making sure Docker is usable, and clearing up what a job leaves behind.

```text
host
 └── runnerly-agent
      └── actions/runner            ← runs on the host
           └── docker
                ├── job container   ← your steps run here, if the workflow says so
                └── service containers
```

If you want every job in a container whether or not the workflow asks, this is
not that. See [what is not here](#what-is-not-here).

## Cleanup

A runner that never cleans up fills its disk with stopped containers, orphaned
volumes and dead networks. Runnerly removes what each job created:

```yaml
executor:
  type: docker
  docker:
    cleanup: after_job     # or: never
    prune_images: false
```

**Only what the job created.** Runnerly records the containers, volumes and
networks that exist when a job starts, and afterwards removes what appeared
since. It does not run `docker system prune`, because a runner is often not
the only thing on its machine and a blanket prune would take someone else's
stopped container with it.

`prune_images` additionally removes dangling images. It is off by default:
image layers are cache, and re-pulling them costs more time than the disk is
usually worth.

Cleanup runs in the job-completed hook, after your steps and before the runner
reports the job finished.

## How it knows

GitHub's runner can call a script when a job starts and when it finishes.
Runnerly installs two, into `<runner dir>/.runnerly/`, and points the runner at
them with `ACTIONS_RUNNER_HOOK_JOB_STARTED` and `..._COMPLETED`.

They are rewritten every time the agent starts, so they never point at a
binary that has moved.

That mechanism also answers something the agent could not previously know:
whether the runner is **busy**. Before this, the agent could see the runner
process was alive but not whether it was running anything, so a machine at
full load reported "online". Now `busy` is what the runner itself said.

With `executor.type: host` no hooks are installed, and the agent reports
`online` rather than guessing.

### Hooks never fail your job

The runner fails a job if a hook exits non-zero. Runnerly's hooks always exit
0. If the daemon is unreachable, the state file is unwritable, or anything
else goes wrong, the problem is printed to the job log and the job carries on.

A cleanup failure also still marks the runner idle, so a stuck volume cannot
leave it looking busy for ever.

## Security

**Read this before pointing a Docker runner at anything you care about.**

Docker is not a security boundary in the way a virtual machine is. Containers
share the host kernel, and a kernel vulnerability is a path out.

More immediately: **anything that can reach the Docker socket has root on the
host.** It can start a container that mounts `/`. So:

- Adding the runner's user to the `docker` group gives that user root. On a
  shared machine, that is a real decision, not a formality.
- A workflow that mounts the Docker socket into a job container hands the host
  to whatever runs in it.
- Rootless Docker is a genuine improvement here. Point `executor.docker.host`
  at its socket.

Runnerly does not pretend otherwise. What the Docker executor buys you is a
cleaner environment between jobs, not isolation from the host.

If you need a real boundary, run one job per disposable virtual machine. That
is not implemented; see [security.md](security.md).

## Non-default sockets

```yaml
executor:
  docker:
    host: unix:///run/user/1000/docker.sock
```

This is passed to the runner as `DOCKER_HOST`, so the containers your
workflows start and the cleanup Runnerly does both reach the same daemon.

## Checking a machine

```bash
runnerly doctor
```

For the Docker executor it checks the CLI is installed, the daemon answers,
and there is disk left where Docker stores its data:

```text
✓ docker
✓ docker daemon
! docker disk space
  6.2 GiB free of 100.0 GiB on /var/lib/docker
  Image layers accumulate quickly on a runner.
  Try:
    docker system prune
```

It warns under 10 GiB and fails under 2 GiB, because a build that dies part
way through an image pull is a confusing way to learn the disk is full.

On Docker Desktop the daemon's storage lives inside its virtual machine, so
the free space is not readable from the host and the check skips saying so
rather than reporting a number about the wrong filesystem.

## Verifying it

A workflow that exercises the containerized path:

```yaml
name: test
on: push

jobs:
  test:
    runs-on: [self-hosted, linux, x64]
    container: node:22
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_PASSWORD: test

    steps:
      - uses: actions/checkout@v4
      - run: npm ci
      - run: npm test
```

While it runs, the dashboard shows the runner as busy with the repository and
workflow name. Afterwards:

```bash
docker ps -a        # the job and service containers are gone
docker volume ls    # so are their volumes
```

and anything that was on the machine before the job is still there.

## What is not here

- **Forcing every job into a container.** GitHub's runner supports this
  through container hooks (`ACTIONS_RUNNER_CONTAINER_HOOKS`), which is a
  whole protocol of its own. Runnerly uses the job hooks, not the container
  hooks.
- **Resource limits.** A job container's CPU and memory are the workflow's to
  set, via `container.options`.
- **Registry credentials.** Use `docker/login-action` in the workflow, or log
  in as the runner's user on the machine.
- **Running the runner itself in a container.** The agent is built to
  supervise a runner on a host. An image exists in `deploy/docker/` but that
  path is not exercised.
