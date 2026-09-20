# Troubleshooting

Start with:

```bash
runnerly doctor
```

Every failing check names the problem and suggests a command. If that is not
enough, the cases below cover the rest.

## doctor says the Docker daemon did not respond

```text
✗ docker daemon
  Docker is installed but the daemon did not respond.
  Cannot connect to the Docker daemon at unix:///var/run/docker.sock.
```

The CLI is installed but `dockerd` is not running, or your user cannot reach
its socket.

```bash
sudo systemctl start docker
sudo systemctl enable docker
```

If the daemon is running and the error is a permissions one, add your user to
the `docker` group and start a new login session:

```bash
sudo usermod -aG docker "$USER"
```

Adding a user to the `docker` group is equivalent to giving them root on that
machine. On a shared host, prefer `executor.type: host`.

## doctor requires Docker and I do not want it

Set the host executor:

```yaml
executor:
  type: host
```

The Docker checks are then skipped.

## doctor fails on outbound HTTPS or the GitHub API

The machine cannot open a TCP connection to `api.github.com:443`.

- Check a proxy: Runnerly honors `HTTPS_PROXY` and `NO_PROXY`.
- Check DNS: `getent hosts api.github.com`.
- Check egress firewall rules; runners need outbound 443.
- Check <https://www.githubstatus.com> if the connection succeeds but the API
  returns 5xx.

To confirm everything else while the network is unavailable:

```bash
runnerly doctor --offline
```

## doctor is slow

Each check is bounded by `--timeout` (default 5s). On a machine with a
black-holed egress route, the network checks will each take the full timeout.

```bash
runnerly doctor --timeout 2s
```

## Configuration changes have no effect

Confirm which file is being read:

```bash
runnerly config path
runnerly config show
```

`config show` prints the merged result. Resolution order is in
[configuration.md](configuration.md); a `$RUNNERLY_CONFIG` left over in a
shell profile is the usual surprise.

## Configuration fails to load with a key error

```text
Error: parse config /home/me/.config/runnerly/config.yaml: ... field executer not found
```

Unknown keys are rejected rather than ignored, so a typo does not silently
disable a setting. Check the spelling against
[configuration.md](configuration.md).

## Output is full of escape characters

Runnerly disables color automatically when stdout is not a terminal. If
something re-enables it, or a log collector shows raw codes:

```bash
runnerly doctor --no-color
NO_COLOR=1 runnerly doctor
```

## doctor warns about the operating system

```text
! operating system
  darwin detected. Runnerly manages runners on Linux only.
```

Expected on macOS. The CLI works for development; register runners from Linux.
This is a warning, not a failure, so the exit code is still `0`.

## GitHub says 404 for a repository that exists

```text
Error: list runners for acme/widgets: GitHub returned 404: Not Found
It does not exist, or the token cannot see it.
```

GitHub returns 404 rather than 403 for resources a token may not access, so a
private repository you cannot administer looks identical to one that is not
there. Check:

```bash
runnerly auth status          # is the right account in use?
runnerly repo list acme       # can the token see it at all?
```

Registering repository runners needs the `repo` scope on a classic token, or
Administration: read and write on a fine-grained one.

## GitHub says 403

Two different problems share this status. Runnerly's message distinguishes
them:

- *"The rate limit resets at ..."* — wait, or authenticate. Unauthenticated
  requests get 60 per hour.
- *"The token needs one of these scopes: ..."* — create a token with the scope
  listed and run `runnerly login` again.

## The wrong token is being used

```bash
runnerly auth status
```

It names the source. Environment variables beat the stored credential, in this
order: `RUNNERLY_GITHUB_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`. A `GITHUB_TOKEN`
exported for another tool is the usual surprise.

## runner create refuses a public repository

```text
Error: acme/widgets is a public repository, and
security.allow_public_repositories is false.
```

Deliberate. A self-hosted runner executes workflow code, so on a public
repository anyone who can get a workflow to run can run commands on that
machine. Read [security.md](security.md). If you still want it, pass
`--allow-public` or set the policy in the configuration.

## runner create says the directory is already configured

```text
Error: ...: this directory already holds a configured runner.
```

Reconfiguring in place would orphan the existing registration in GitHub. Either
retire the old runner:

```bash
runnerly runner remove <name>
rm -rf ~/.local/share/runnerly/runners/<name>
```

or pass `--replace` to take over the registration. `--replace` reuses the
unpacked runner, so it does not download the release again.

## A download fails verification

```text
Error: actions-runner-linux-x64-2.x.x.tar.gz failed verification.
```

The file was deleted rather than executed. Retry — a truncated download is the
common cause. If it fails again with a consistent mismatch, stop and
investigate: something between you and GitHub is altering the file.

## A runner shows offline right after creating it

Expected. `runnerly runner create` registers the runner but does not start it.
Runnerly does not supervise runner processes yet:

```bash
cd ~/.local/share/runnerly/runners/<name> && ./run.sh
```

## Reporting a bug

Include:

```bash
runnerly version
runnerly doctor --json
```

Redact hostnames or URLs you would rather not publish. Security issues go to
[SECURITY.md](../SECURITY.md), not the issue tracker.
