#!/bin/sh
# The guided installer. It installs the netscribe command, then runs "netscribe setup",
# which checks the machine, asks the few questions that matter, writes the config, starts
# the service and creates the first administrator. Run "netscribe doctor" later to check
# the install again.
#
#   sudo sh get.sh                              the latest release, then the guided setup
#   sudo sh get.sh --version v1.2.0             a given release
#   sudo sh get.sh --dry-run                    print what the setup would do, change nothing
#   sudo sh get.sh --yes --admin alice --password-stdin < password.txt
#
# --version, --prefix and --base-url go to install.sh. Every other option goes to
# "netscribe setup", see "netscribe setup -h". The classic installer, install.sh, installs
# the same binary and leaves the configuration to you.
set -eu

RAW_URL="${NETSCRIBE_RAW_URL:-https://raw.githubusercontent.com/FlexEbat/Netscribe/main/scripts}"
VERSION=""
PREFIX="/usr/local"
BASE_URL=""
WORK=""

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [ -n "$WORK" ] && [ -d "$WORK" ]; then
		rm -rf "$WORK"
	fi
}
trap cleanup EXIT INT TERM

usage() {
	cat <<'USAGE'
usage: get.sh [options]

  --version TAG     install this release, for example v1.2.0 (default: the latest)
  --prefix DIR      install the binary to DIR/bin (default: /usr/local)
  --base-url URL    download releases from URL/TAG/ instead of GitHub

Every other option goes to "netscribe setup", for example --dry-run, --yes,
--admin NAME and --password-stdin. See "netscribe setup -h".
USAGE
}

# Options for install.sh are taken out of the list. The rest is kept, in order, for
# "netscribe setup": each one moves from the front to the back, and after one turn the
# list holds only those.
turns=$#
while [ "$turns" -gt 0 ]; do
	case "$1" in
	--version | --prefix | --base-url)
		[ $# -ge 2 ] || die "$1 needs a value"
		case "$1" in
		--version) VERSION="$2" ;;
		--prefix) PREFIX="$2" ;;
		--base-url) BASE_URL="$2" ;;
		esac
		shift 2
		turns=$((turns - 2))
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		set -- "$@" "$1"
		shift
		turns=$((turns - 1))
		;;
	esac
done

[ "$(uname -s)" = "Linux" ] || die "Netscribe runs on Linux only"
[ "$(id -u)" -eq 0 ] || die "run the guided installer as root, for example: sudo sh get.sh"

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "neither curl nor wget is installed: install one and run this again"
	fi
}

# The installer next to this script is used when the script runs from a checkout or a
# download. Piped from curl, the script fetches install.sh itself.
INSTALL=""
case "$0" in
*/get.sh | get.sh)
	dir="$(cd "$(dirname "$0")" && pwd)"
	if [ -f "$dir/install.sh" ]; then
		INSTALL="$dir/install.sh"
	fi
	;;
esac
if [ -z "$INSTALL" ]; then
	WORK="$(mktemp -d)"
	fetch "$RAW_URL/install.sh" "$WORK/install.sh" || die "cannot download $RAW_URL/install.sh"
	INSTALL="$WORK/install.sh"
fi

# A function has its own argument list, so the options of "netscribe setup" stay in $@ here.
run_installer() {
	set -- --no-service --prefix "$PREFIX"
	if [ -n "$VERSION" ]; then
		set -- "$@" --version "$VERSION"
	fi
	if [ -n "$BASE_URL" ]; then
		set -- "$@" --base-url "$BASE_URL"
	fi
	NETSCRIBE_GUIDED=1 sh "$INSTALL" "$@"
}
run_installer

BIN="$PREFIX/bin/netscribe"
[ -x "$BIN" ] || die "the installer did not leave $BIN"

# Piped from curl, standard input is this script, not the keyboard. The questions of the
# setup go to the terminal then, unless the person feeds the answers on purpose.
piped=1
for a in "$@"; do
	case "$a" in
	--yes | --password-stdin) piped=0 ;;
	esac
done
if [ ! -t 0 ] && [ "$piped" -eq 1 ] && (: </dev/tty) 2>/dev/null; then
	exec "$BIN" setup "$@" </dev/tty
fi
exec "$BIN" setup "$@"
