# Installation

## Supported platforms

| Platform | State |
| --- | --- |
| Linux x86_64 | developed and tested here |
| Linux arm64 | developed and tested here |
| macOS | supported; see [macos.md](macos.md) |
| Windows | not supported yet; see [windows.md](windows.md) |

Ubuntu and Debian are the first-class targets; the remediation commands
`doctor` prints assume `apt` and `systemd`.

macOS is supported: runners register, take jobs and come back after a
reboot. `runnerly agent launchd` writes the LaunchAgent, which is the
service manager a Mac actually has. Two things behave differently enough
to be worth reading before you set one up — protected folders and PATH —
and both are in [macos.md](macos.md).

Windows is not supported yet, and there is no installer for it. The
platform pieces — unpacking the release, the job hooks, stopping a runner
gracefully, the machine readings, a scheduled task — are implemented and
checked by CI on a real Windows machine. What is missing is an end-to-end
run: nobody has watched a Windows runner take a job. [windows.md](windows.md)
is the full list of what does and does not work.

## The installer

```bash
curl -fsSL https://runnerly.dev/install.sh | sh
```

It works out the platform, downloads the matching release archive and the
`SHA256SUMS` published beside it, refuses to continue if they disagree, and
copies three binaries onto your PATH. Then:

```bash
runnerly setup
```

**This needs a published release.** The release workflow builds and uploads
the archives, with a `SHA256SUMS` beside them, when a `v*` tag is pushed. If
the installer cannot find a release, or the repository is private to you,
build from source instead.

What the installer will not do: run `sudo` on your behalf, write outside the
install directory, register a runner, or start anything. If `/usr/local/bin`
is not writable it installs into `~/.local/bin` instead and tells you; if you
pointed it somewhere it cannot write, it stops and prints the two ways to fix
it.

| Variable | Effect |
| --- | --- |
| `RUNNERLY_VERSION` | install this version instead of the latest |
| `RUNNERLY_PREFIX` | install prefix; binaries go in `$RUNNERLY_PREFIX/bin` |
| `RUNNERLY_REPO` | the `owner/repo` to download from |
| `RUNNERLY_BASE_URL` | a mirror or an air-gapped host holding the same file names and a `SHA256SUMS`. Needs `RUNNERLY_VERSION` too, since a mirror has no "latest" to ask for |

Piping a script into a shell means trusting the source. To read it first:

```bash
curl -fsSL https://runnerly.dev/install.sh -o install.sh
less install.sh
sh install.sh
```

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

No package manager:

```bash
# Not implemented
brew install runnerly
apt install runnerly
```

`install.sh` and building from source are the two paths. Runnerly also does
not replace its own binary: `runnerly upgrade` says when a newer version
exists and leaves the swap to you, which for an installed release means
running the installer again.

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
