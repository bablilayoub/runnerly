#!/bin/sh
#
# Install Runnerly.
#
#   curl -fsSL https://runnerly.dev/install.sh | sh
#
# What it does: works out this platform, downloads the matching release
# archive and the checksum file published with it, refuses to continue if
# they disagree, and copies three binaries into a directory on your PATH.
#
# What it does not do: run sudo behind your back, write outside the install
# directory, register a runner, or start anything. When it needs root it says
# so and stops, printing the one command to re-run.
#
# Environment:
#   RUNNERLY_VERSION   version to install (default: the latest release)
#   RUNNERLY_PREFIX    install prefix; binaries go in $RUNNERLY_PREFIX/bin
#   RUNNERLY_REPO      owner/repo to download from
#   RUNNERLY_BASE_URL  where the archives live, for a mirror or an air-gapped
#                      host. Must hold the same file names and a SHA256SUMS.
#
# Everything below is wrapped in main() and called on the last line, so a
# truncated download cannot execute half a script.

set -eu

REPO="${RUNNERLY_REPO:-bablilayoub/runnerly}"
BINARIES="runnerly runnerly-agent runnerly-server"

say() { printf '%s\n' "$*"; }
step() { printf '\033[2m  %s\033[0m\n' "$*"; }
ok() { printf '\033[32m✓\033[0m %s\n' "$*"; }

die() {
	printf '\033[31m✗\033[0m %s\n' "$1" >&2
	shift
	for line in "$@"; do printf '  %s\n' "$line" >&2; done
	exit 1
}

# No color when stdout is not a terminal, so piping into a log stays readable.
if [ ! -t 1 ]; then
	say() { printf '%s\n' "$*"; }
	step() { printf '  %s\n' "$*"; }
	ok() { printf '%s\n' "OK: $*"; }
	die() {
		printf '%s\n' "ERROR: $1" >&2
		shift
		for line in "$@"; do printf '  %s\n' "$line" >&2; done
		exit 1
	}
fi

have() { command -v "$1" >/dev/null 2>&1; }

detect_platform() {
	os="$(uname -s)"
	arch="$(uname -m)"

	case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*)
		die "Runnerly has no build for $os." \
			"Linux and macOS are the supported platforms." \
			"Build from source: https://github.com/$REPO#quick-start"
		;;
	esac

	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*)
		die "Runnerly has no build for $arch." \
			"Supported: x86_64 and arm64." \
			"Build from source: https://github.com/$REPO#quick-start"
		;;
	esac

	# The one combination that is not built. Saying so here beats a 404 from
	# the download.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ]; then
		die "There is no darwin/amd64 build." \
			"Intel Macs are not a release target; build from source:" \
			"https://github.com/$REPO#quick-start"
	fi

	PLATFORM="${os}_${arch}"
}

check_tools() {
	if have curl; then
		DOWNLOAD='curl -fsSL -o'
	elif have wget; then
		DOWNLOAD='wget -qO'
	else
		die "Neither curl nor wget is installed." \
			"Install one of them and run this again."
	fi

	if have sha256sum; then
		SHASUM='sha256sum'
	elif have shasum; then
		SHASUM='shasum -a 256'
	else
		die "No sha256 tool was found (sha256sum or shasum)." \
			"Runnerly will not install an archive it cannot verify."
	fi

	have tar || die "tar is not installed." "Install it and run this again."
}

fetch() { $DOWNLOAD "$1" "$2"; }

resolve_version() {
	if [ -n "${RUNNERLY_VERSION:-}" ]; then
		VERSION="$RUNNERLY_VERSION"
		return
	fi
	if [ -n "${RUNNERLY_BASE_URL:-}" ]; then
		die "RUNNERLY_BASE_URL is set but RUNNERLY_VERSION is not." \
			"A mirror has no \"latest\" to ask for, so name the version:" \
			"  RUNNERLY_VERSION=v0.1.0 RUNNERLY_BASE_URL=... sh install.sh"
	fi

	step "resolving the latest release"
	# The redirect on /releases/latest carries the tag, so this needs no JSON
	# parser and no API token.
	if have curl; then
		location="$(curl -fsSI "https://github.com/$REPO/releases/latest" |
			tr -d '\r' | awk 'tolower($1) == "location:" { print $2 }' | tail -1)"
	else
		location="$(wget -qS --spider "https://github.com/$REPO/releases/latest" 2>&1 |
			tr -d '\r' | awk 'tolower($1) == "location:" { print $2 }' | tail -1)"
	fi

	VERSION="${location##*/}"
	case "$VERSION" in
	v*) ;;
	*)
		die "Could not work out the latest version." \
			"If this repository is private, releases are not readable without a token." \
			"Pick one yourself: RUNNERLY_VERSION=v0.1.0 sh install.sh" \
			"Or build from source: https://github.com/$REPO#quick-start"
		;;
	esac
}

# choose_prefix picks somewhere to install without ever escalating on its own.
choose_prefix() {
	if [ -n "${RUNNERLY_PREFIX:-}" ]; then
		PREFIX="$RUNNERLY_PREFIX"
		return
	fi
	if [ -w /usr/local/bin ] 2>/dev/null; then
		PREFIX=/usr/local
		return
	fi
	if [ "$(id -u)" = 0 ]; then
		PREFIX=/usr/local
		return
	fi
	# Not root and /usr/local/bin is not writable. A per-user install works
	# without asking anyone for a password.
	PREFIX="$HOME/.local"
	USER_INSTALL=yes
}

download_and_verify() {
	archive="runnerly_${VERSION}_${PLATFORM}.tar.gz"
	base="${RUNNERLY_BASE_URL:-https://github.com/$REPO/releases/download/$VERSION}"

	step "downloading $archive"
	fetch "$TMP/$archive" "$base/$archive" ||
		die "Could not download $archive." \
			"Tried: $base/$archive" \
			"Check that $VERSION exists and has a build for $PLATFORM."

	step "downloading SHA256SUMS"
	fetch "$TMP/SHA256SUMS" "$base/SHA256SUMS" ||
		die "Could not download SHA256SUMS." \
			"Runnerly will not install an archive it cannot verify."

	step "verifying the checksum"
	# Pull out the one line for this archive. SHA256SUMS lists every platform
	# and the others were never downloaded, so checking the whole file would
	# fail on files that were never meant to be here.
	#
	# The name is matched as a whole field rather than with a pattern: the
	# archive name is full of dots, and a regex would let a neighboring name
	# match. awk also reports whether the name was there at all, which the
	# checksum tools do not agree on — some exit 0 when handed no lines, so an
	# archive missing from SHA256SUMS would otherwise pass as verified.
	if ! awk -v want="$archive" \
		'$2 == want || $2 == "*" want { print; found = 1 }
		 END { exit found ? 0 : 1 }' \
		"$TMP/SHA256SUMS" > "$TMP/expected.sha256"; then
		die "SHA256SUMS does not list $archive." \
			"Runnerly will not install an archive that was never signed for." \
			"Nothing was installed."
	fi

	if ! (cd "$TMP" && $SHASUM -c expected.sha256 >/dev/null 2>&1); then
		die "$archive does not match its published checksum." \
			"The download is corrupt or has been tampered with. Nothing was installed."
	fi
	ok "checksum verified"
}

install_binaries() {
	step "unpacking"
	tar -C "$TMP" -xzf "$TMP/$archive"
	unpacked="$TMP/runnerly_${VERSION}_${PLATFORM}"

	for binary in $BINARIES; do
		[ -f "$unpacked/$binary" ] ||
			die "$archive did not contain $binary." "Nothing was installed."
	done

	bindir="$PREFIX/bin"
	if ! mkdir -p "$bindir" 2>/dev/null; then
		die "Cannot create $bindir." \
			"Re-run with a prefix you own:" \
			"  RUNNERLY_PREFIX=\"\$HOME/.local\" sh install.sh"
	fi
	if [ ! -w "$bindir" ]; then
		die "$bindir is not writable by $(id -un)." \
			"Runnerly does not call sudo for you. Either run:" \
			"  curl -fsSL https://runnerly.dev/install.sh | sudo sh" \
			"or install it under your own home directory:" \
			"  RUNNERLY_PREFIX=\"\$HOME/.local\" sh install.sh"
	fi

	for binary in $BINARIES; do
		install -m 0755 "$unpacked/$binary" "$bindir/$binary" 2>/dev/null ||
			# BusyBox install does not always accept -m.
			{ cp "$unpacked/$binary" "$bindir/$binary" && chmod 0755 "$bindir/$binary"; } ||
			die "Could not install $binary into $bindir."
		step "installed $bindir/$binary"
	done
}

on_path() {
	case ":$PATH:" in
	*":$1:"*) return 0 ;;
	*) return 1 ;;
	esac
}

main() {
	say ""
	say "Runnerly installer"
	say ""

	detect_platform
	check_tools
	resolve_version
	choose_prefix

	step "$VERSION, $PLATFORM, into $PREFIX/bin"

	TMP="$(mktemp -d)"
	trap 'rm -rf "$TMP"' EXIT INT TERM

	download_and_verify
	install_binaries

	say ""
	ok "Runnerly $VERSION is installed"
	say ""

	if [ "${USER_INSTALL:-}" = yes ] && ! on_path "$PREFIX/bin"; then
		say "$PREFIX/bin is not on your PATH. Add it:"
		say ""
		say "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.profile"
		say "  export PATH=\"\$HOME/.local/bin:\$PATH\""
		say ""
	fi

	say "Next:"
	say ""
	say "  runnerly setup"
	say ""
	say "That checks the machine, signs in to GitHub, registers a runner and"
	say "prints the commands to keep it running. Nothing is changed until you"
	say "have seen what it is going to do."
	say ""
}

main "$@"
