# Runners

A runner is GitHub's official Actions runner, installed and registered by
Runnerly. Runnerly manages its lifecycle; it does not replace it.

## Create

```bash
runnerly runner create --repo acme/widgets
```

What happens:

1. Check the security policy for the target (see below).
2. Ask GitHub for a short-lived registration token.
3. Ask GitHub which runner release it expects for this platform.
4. Download that release and verify the SHA-256 checksum GitHub published with
   it. A file that fails verification is deleted, not executed.
5. Unpack it, rejecting any archive entry that would be written outside the
   install directory.
6. Run `config.sh --unattended` with the token, name and labels.

The runner is **registered but not started**. Supervising the process is the
agent's job, which is not implemented yet:

```bash
cd ~/.local/share/runnerly/runners/runnerly-01 && ./run.sh
```

### Options

| Flag | Meaning |
| --- | --- |
| `--repo`, `--org` | where to register; falls back to the configuration |
| `--name` | runner name in GitHub; defaults to `runner.name`, else the hostname |
| `--labels` | comma-separated custom labels; defaults to `runner.labels` |
| `--dir` | install location; defaults to `runner.dir` plus the runner name |
| `--work` | the runner's working directory (default `_work`) |
| `--group` | runner group to join, for organization runners |
| `--ephemeral` | accept one job, then deregister |
| `--replace` | take over an existing registration with the same name |
| `--allow-public` | override the public-repository policy for this run |

### Install location

`runner.dir` in the configuration, plus the runner name. When it is empty:

- root: `/opt/runnerly/runners/<name>`
- anyone else: `~/.local/share/runnerly/runners/<name>`

Runnerly never writes outside what the invoking user already owns.

### Re-registering

A directory that already holds a configured runner is refused:

```text
Error: /home/me/.local/share/runnerly/runners/runnerly-01: this directory
already holds a configured runner.
Remove it with `runnerly runner remove runnerly-01` and delete the directory,
or pass --replace
```

Silently reconfiguring would leave an orphaned registration in GitHub.
`--replace` reuses the unpacked runner rather than downloading it again.

## Labels

Labels map directly to GitHub's. Runnerly adds no routing system of its own.

Runnerly always sends the platform labels GitHub's runner would apply anyway
(`self-hosted`, the OS, the architecture) together with your custom ones, so the
labels in your configuration are the whole truth about a runner rather than a
partial list GitHub silently extends.

```yaml
runner:
  labels:
    - docker
    - node
```

registers `self-hosted,linux,x64,docker,node` on a Linux x86_64 machine, and a
workflow reaches it with:

```yaml
jobs:
  test:
    runs-on: [self-hosted, linux, x64, docker]
```

## List

```bash
runnerly runner list
runnerly runner list --org acme
runnerly runner list --json
```

```text
NAME         ID  STATUS   OS     LABELS
runnerly-01  7   online   linux  self-hosted,linux,x64,docker
runnerly-02  8   busy     linux  self-hosted,linux,x64
runnerly-03  9   offline  linux  self-hosted,linux,arm64
```

This is GitHub's view, not Runnerly's. It includes runners Runnerly did not
create, and a runner shows as offline whenever its machine is not connected.

## Status

```bash
runnerly runner status runnerly-01
```

## Remove

```bash
runnerly runner remove runnerly-01
runnerly runner remove --id 7 --yes
```

This deletes the **registration in GitHub**. It does not stop a runner process
that is still running, and it deletes nothing from the machine. A still-running
runner whose registration was removed will fail to reconnect.

To retire a runner completely:

```bash
# on the runner machine
cd ~/.local/share/runnerly/runners/runnerly-01
# stop ./run.sh, then
runnerly runner remove runnerly-01
rm -rf ~/.local/share/runnerly/runners/runnerly-01
```

Removal asks for confirmation. Without a terminal to ask on, it refuses rather
than assuming yes; pass `--yes` in a script.

## Ephemeral runners

`--ephemeral` registers a runner that accepts one job and then deregisters
itself. GitHub recommends this for autoscaling and for a clean environment per
job.

Runnerly passes the flag through to `config.sh`, but does not yet manage the
lifecycle around it: nothing re-creates the runner after its job, and nothing
preserves its logs. Full support is a later milestone.

## What is not implemented

- starting, supervising or restarting the runner process
- automatic recovery after a crash
- upgrades of the runner or of Runnerly itself
- the control plane, heartbeats and the dashboard
- ephemeral lifecycle management

See [architecture.md](architecture.md).
