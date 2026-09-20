# GitHub integration

Runnerly talks to GitHub's REST API to find repositories, issue registration
tokens, and list or remove runners. It never asks GitHub to schedule anything:
GitHub keeps doing that.

## Authenticating

Runnerly uses a personal access token. OAuth device flow needs a registered
OAuth app, which does not exist yet.

```bash
echo "$GITHUB_TOKEN" | runnerly login --with-token
```

The token is read from standard input rather than a flag, so it does not reach
your shell history or show up in `ps`.

Check what is in use:

```bash
runnerly auth status
```

```text
✓ github.com
  account: octocat
  token from /home/me/.config/runnerly/credentials.yaml (file)
  scopes: repo, workflow
```

`auth status` exits `1` when there is no usable credential, so it works in a
script. `--json` gives machine-readable output; `--offline` reports the stored
credential without contacting GitHub.

Remove a stored token:

```bash
runnerly logout
```

This deletes Runnerly's copy. It does **not** revoke the token on GitHub —
revoke it at `https://github.com/settings/tokens` if it may have been exposed.

## Which token

| Runner scope | Classic token | Fine-grained token |
| --- | --- | --- |
| Repository | `repo` | Administration: read and write, on that repository |
| Organization | `admin:org` | Organization permissions: self-hosted runners |

`runnerly doctor` checks a classic token's scopes and warns before you hit a
failure at registration time. It cannot check a fine-grained token: GitHub does
not report scopes for those, so an absent scope proves nothing. Such a token
fails at the point of use with a 403 that says what it needs.

## Where the token comes from

Resolution order, first match wins:

1. `--token` on the command line
2. `RUNNERLY_GITHUB_TOKEN`
3. `GITHUB_TOKEN`
4. `GH_TOKEN`
5. the credentials file

An environment token always beats the stored one, so a CI job can override a
machine's credential without touching it. `runnerly auth status` always says
which source was used.

`--token` is supported because non-interactive setup needs it, but it is the
worst option: a flag is visible in shell history and in process listings.
Prefer the environment.

## Repository discovery

```bash
runnerly repo list
runnerly repo list widgets      # filter by substring
runnerly repo list --all        # include repositories you cannot administer
```

By default only repositories you can administer are listed, because adding a
runner requires admin access.

## Scopes

Every runner command takes `--repo owner/repo` or `--org login`, falling back to
the configuration file:

```yaml
github:
  scope: repository
  repository: acme/widgets
```

`--repo` accepts what people actually paste: `acme/widgets`,
`https://github.com/acme/widgets`, `git@github.com:acme/widgets.git`.

Repository and organization runners are supported. Enterprise runners and
runner groups beyond `--group` are not.

## Registration tokens

Registering a runner needs a short-lived token, separate from your personal
access token. Runnerly requests one per registration and never writes it to
disk; GitHub expires them an hour after issue. `config.sh` exchanges it for the
runner's own credentials, which is why the same token cannot be reused.

## GitHub Enterprise Server

```yaml
github:
  host: github.acme.com
```

Runnerly then uses `https://github.acme.com/api/v3`. This path is implemented
but has not been exercised against a real Enterprise Server instance.

## Rate limits and errors

Runnerly reads GitHub's rate limit headers and distinguishes a rate-limited 403
from a permissions 403, because the fix is different.

A 404 on a runner endpoint usually means the token cannot see the resource, not
that it is absent: GitHub returns 404 rather than 403 to avoid revealing that a
private resource exists. Runnerly's error says so instead of repeating
"Not Found".
