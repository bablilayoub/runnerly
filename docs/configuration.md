# Configuration

Runnerly reads a single YAML file. Every field has a default, and a missing
file is not an error — `runnerly doctor` works on a machine that has never
been configured.

## Where the file lives

Resolution order, first match wins:

1. `--config /path/to/config.yaml`
2. `$RUNNERLY_CONFIG`
3. `$XDG_CONFIG_HOME/runnerly/config.yaml`, or `~/.config/runnerly/config.yaml`
4. `/etc/runnerly/config.yaml`, if it exists and the per-user file does not

```bash
runnerly config path
```

The file is written with mode `0600` because it will hold credentials in a
later release.

## Commands

```bash
runnerly config init        # write a default file (--force to overwrite)
runnerly config show        # effective settings, defaults included
runnerly config validate    # check the file before relying on it
```

`config show` prints the merged result, so it is the authoritative answer to
"what is Runnerly actually using?".

Unknown keys are a hard error. A typo like `executer:` fails loudly at load
time rather than being silently ignored.

## Reference

```yaml
server:
  # Runnerly control plane. Optional: the CLI works without one.
  # Must start with http:// or https:// when set.
  url: ""

github:
  # GitHub hostname. Use github.com unless you run GitHub Enterprise Server.
  host: github.com
  # "repository" or "organization".
  scope: repository
  # owner/repo, when scope is repository.
  repository: ""
  # org login, required when scope is organization.
  organization: ""

runner:
  # Runner name as it appears in GitHub. Letters, digits, '.', '_', '-'.
  # Empty means "generate one".
  name: ""
  # GitHub runner labels. These map directly to GitHub's labels; Runnerly
  # does not add a routing system of its own. The platform labels
  # (self-hosted, the OS, the architecture) are always added on top.
  labels:
    - linux
    - x64
    - runnerly
  # Where the official GitHub runner is installed. Empty uses
  # /opt/runnerly/runners for root, or ~/.local/share/runnerly/runners.
  dir: ""

executor:
  # "host"   - the GitHub runner executes jobs directly on the machine
  # "docker" - the machine runs the runner with Docker available for
  #            containerized steps and job containers
  type: docker

updates:
  # Whether Runnerly may upgrade itself and the GitHub runner unattended.
  auto: false

security:
  # See docs/security.md. These are currently advisory: Runnerly surfaces
  # them, it does not yet enforce them against GitHub.
  allow_public_repositories: false
  allow_fork_workflows: false
```

### Defaults

`runner.labels` defaults to the machine's own OS and architecture plus an
`runnerly` marker, so Runnerly-managed runners are identifiable in the
GitHub UI. On Linux x86_64 that is `linux`, `x64`, `runnerly` — matching the
names GitHub uses, not Go's (`amd64` becomes `x64`).

`executor.type` defaults to `docker`. If Docker is not installed and you do not
want it, set `type: host` and `doctor` stops requiring Docker.

## Effect on doctor

The configuration changes what `doctor` checks:

| Setting | Effect |
| --- | --- |
| `executor.type: host` | the Docker checks are skipped |
| `executor.type: docker` | a missing Docker CLI is a failure |
| `server.url: ""` | the control-plane check is skipped |
| `server.url` set | `GET <url>/api/v1/health` must return 200 |
| `github.host` | chooses the API endpoint that must be reachable |
| `github.scope` | chooses which token scope the credentials check expects |

## Credentials

The GitHub token is **not** in this file. It lives in `credentials.yaml` beside
it, written with mode 0600, and is managed with `runnerly login` and
`runnerly logout`. See [github.md](github.md) and [security.md](security.md).

## Not yet used

`server.url` and `updates.auto` are read and validated but not yet acted on —
the components that consume them do not exist. They are in the file now so the
shape of the configuration does not change under you later.

`security.allow_fork_workflows` is likewise recorded but not enforced;
`security.allow_public_repositories` **is** enforced, by
`runnerly runner create`.
