# Installation

## Supported platforms

| Platform | State |
| --- | --- |
| Linux x86_64 | developed and tested here |
| Linux arm64 | developed and tested here |
| macOS | runners work; no service integration |
| Windows | not supported |

Ubuntu and Debian are the first-class targets; the remediation commands
`doctor` prints assume `apt` and `systemd`.

A runner does register and run real jobs on macOS. What is missing is the
service integration: `runnerly agent systemd` generates a systemd unit, which
macOS does not use, so run the agent in the foreground or keep it under
launchd yourself. `doctor` says so and still exits `0`.

Windows is not supported. The job hooks Runnerly installs are shell scripts,
so cleanup between jobs and `busy` reporting do not work there.

## From source

Requires Go 1.25 or newer, and Node 22 or newer for the dashboard.

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make all
```

Three binaries land in `dist/`:

| Binary | What it is |
| --- | --- |
| `runnerly` | the CLI |
| `runnerly-agent` | the daemon systemd runs |
| `runnerly-server` | the control plane, with the dashboard embedded |

`make build` skips the dashboard and needs no Node. The CLI and the agent are
identical either way; `runnerly-server` built that way serves a page saying
the dashboard is missing, and its API is unaffected.

To install system-wide:

```bash
sudo make install
```

That copies all three to `/usr/local/bin`. Override the location with
`PREFIX`:

```bash
make install PREFIX="$HOME/.local"
```

`make install` is the only target that needs elevated privileges, and only
because `/usr/local/bin` is usually root-owned.

## Cross-compiling

```bash
GOOS=linux GOARCH=arm64 make build
```

Run `make web` first if that build should carry the dashboard; the assets are
platform-independent, so one build of them serves every target.

## Verify

```bash
runnerly version
runnerly doctor
```

## Not available yet

The one-line installer and package-manager builds described in the project plan
do not exist yet:

```bash
# Not implemented
curl -fsSL https://runnerly.dev/install.sh | sh
brew install runnerly
```

Building from source is the only supported path today. When an installer does
ship it will detect the platform, verify a checksum, and explain any step that
needs root before taking it.

## Uninstall

Remove the runners first, so nothing is left registered in GitHub:

```bash
runnerly runner remove <name> --purge
```

Then:

```bash
sudo rm /usr/local/bin/runnerly /usr/local/bin/runnerly-agent /usr/local/bin/runnerly-server
rm -rf ~/.config/runnerly          # config, credentials, runners.yaml
rm -rf ~/.local/share/runnerly     # installed runners
```

A systemd unit you installed by hand is yours to remove:
`sudo systemctl disable --now runnerly-agent@<name>` and delete the unit file.
