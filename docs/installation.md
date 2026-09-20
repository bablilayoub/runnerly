# Installation

## Supported platforms

| Platform | State |
| --- | --- |
| Linux x86_64 | target platform |
| Linux arm64 | target platform |
| macOS | CLI only, for development |
| Windows | not supported |

Runnerly manages runners on Linux. The CLI builds and runs on macOS so you
can develop against it, but `doctor` will warn that a runner registered from
that machine is not supported.

Ubuntu and Debian are the first-class targets; the remediation commands
`doctor` prints assume `apt` and `systemd`.

## From source

Requires Go 1.24 or newer.

```bash
git clone https://github.com/bablilayoub/runnerly.git
cd runnerly
make build
```

The binary lands in `dist/runnerly`. To install it system-wide:

```bash
sudo make install
```

That copies the binary to `/usr/local/bin/runnerly`. Override the location
with `PREFIX`:

```bash
make install PREFIX="$HOME/.local"
```

`make install` is the only target that needs elevated privileges, and only
because `/usr/local/bin` is usually root-owned.

## Cross-compiling

```bash
GOOS=linux GOARCH=arm64 make build
```

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

```bash
sudo rm /usr/local/bin/runnerly
rm -rf ~/.config/runnerly
```
