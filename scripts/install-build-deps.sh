#!/bin/sh
# Installs what building Netscribe from source needs: git, make, Go and Node.js.
#
# git and make come from the package manager of the host (apt, dnf, yum, zypper, pacman,
# apk or xbps). Go comes from the official archive of the version that go.mod asks for,
# and Node.js from the official archive of the current release of a major version. Both
# archives are checked against the published checksums. On musl systems such as Alpine the
# official Node.js build does not run, so Node.js comes from the package manager there.
#
#   sh scripts/install-build-deps.sh                 into /usr/local as root, ~/.local otherwise
#   sh scripts/install-build-deps.sh --prefix /opt/netscribe-tools
#   sh scripts/install-build-deps.sh --dry-run       print the plan and download nothing
set -eu

PREFIX=""
NODE_MAJOR="22"
GO_MOD="go.mod"
DRY_RUN=0
WORK=""
PM_UPDATED=0

say() { printf '%s\n' "$*"; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'USAGE'
usage: install-build-deps.sh [options]

  --prefix DIR        install Go and Node.js below DIR (default: /usr/local as root, ~/.local otherwise)
  --node-major N      Node.js major version (default: 22)
  --go-mod FILE       read the Go version from FILE (default: go.mod)
  --dry-run           print the plan and download nothing
  -h, --help          show this text
USAGE
}

cleanup() {
	if [ -n "$WORK" ] && [ -d "$WORK" ]; then
		rm -rf "$WORK"
	fi
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix)
		[ $# -ge 2 ] || die "--prefix needs a value"
		PREFIX="$2"
		shift
		;;
	--node-major)
		[ $# -ge 2 ] || die "--node-major needs a value"
		NODE_MAJOR="$2"
		shift
		;;
	--go-mod)
		[ $# -ge 2 ] || die "--go-mod needs a value"
		GO_MOD="$2"
		shift
		;;
	--dry-run) DRY_RUN=1 ;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		die "unknown option: $1"
		;;
	esac
	shift
done

[ "$(uname -s)" = "Linux" ] || die "Netscribe builds on Linux only"

if [ -z "$PREFIX" ]; then
	if [ "$(id -u)" -eq 0 ]; then
		PREFIX="/usr/local"
	else
		PREFIX="$HOME/.local"
	fi
fi

# sudo_if_needed runs a command as root when the current user is not root.
sudo_if_needed() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif command -v sudo >/dev/null 2>&1; then
		sudo "$@"
	else
		die "installing packages needs root: run as root or install sudo"
	fi
}

detect_pm() {
	for pm in apt-get dnf yum zypper pacman apk xbps-install; do
		if command -v "$pm" >/dev/null 2>&1; then
			printf '%s' "$pm"
			return 0
		fi
	done
	return 1
}

pm_install() {
	pm="$(detect_pm)" || return 1
	case "$pm" in
	apt-get)
		if [ "$PM_UPDATED" -eq 0 ]; then
			sudo_if_needed apt-get update -qq
			PM_UPDATED=1
		fi
		sudo_if_needed env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
		;;
	dnf) sudo_if_needed dnf install -y -q "$@" ;;
	yum) sudo_if_needed yum install -y -q "$@" ;;
	zypper) sudo_if_needed zypper --non-interactive --quiet install "$@" ;;
	pacman) sudo_if_needed pacman -S --noconfirm --needed "$@" ;;
	apk) sudo_if_needed apk add --no-cache --quiet "$@" ;;
	xbps-install) sudo_if_needed xbps-install -Sy "$@" ;;
	esac
}

ensure_tool() {
	command -v "$1" >/dev/null 2>&1 && return 0
	say "installing $1"
	# shellcheck disable=SC2086 # the package list is a word list on purpose
	pm_install $2 || die "$1 is missing and could not be installed: install it and run this script again"
	command -v "$1" >/dev/null 2>&1 || die "$1 is still missing after the install"
}

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	else
		wget -q -O "$2" "$1"
	fi
}

# go_version prints the Go version go.mod asks for: the toolchain line wins over the go line.
go_version() {
	[ -f "$GO_MOD" ] || die "$GO_MOD not found: run this script in the repository or pass --go-mod"
	v="$(awk '$1 == "toolchain" { sub(/^go/, "", $2); print $2; exit }' "$GO_MOD")"
	if [ -z "$v" ]; then
		v="$(awk '$1 == "go" { print $2; exit }' "$GO_MOD")"
	fi
	[ -n "$v" ] || die "no go or toolchain line in $GO_MOD"
	printf '%s' "$v"
}

is_musl() {
	ls /lib/ld-musl-* >/dev/null 2>&1
}

case "$(uname -m)" in
x86_64 | amd64)
	GOARCH="amd64"
	NODEARCH="x64"
	;;
aarch64 | arm64)
	GOARCH="arm64"
	NODEARCH="arm64"
	;;
*) die "no official Go and Node.js archives for $(uname -m): install them with your package manager" ;;
esac

GOVER="$(go_version)"
GOFILE="go${GOVER}.linux-${GOARCH}.tar.gz"
GOURL="https://dl.google.com/go/$GOFILE"

say "plan: git and make from the package manager"
say "plan: Go $GOVER from $GOURL into $PREFIX/go"
if is_musl; then
	say "plan: Node.js $NODE_MAJOR from the package manager (musl)"
else
	say "plan: Node.js $NODE_MAJOR from nodejs.org into $PREFIX/node"
fi
if [ "$DRY_RUN" -eq 1 ]; then
	exit 0
fi

ensure_tool git "git"
ensure_tool make "make"
if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
	ensure_tool curl "curl ca-certificates"
fi
ensure_tool tar "tar"
ensure_tool sha256sum "coreutils"

WORK="$(mktemp -d)"
mkdir -p "$PREFIX/bin"

# Go
fetch "$GOURL" "$WORK/$GOFILE" || die "cannot download $GOURL"
fetch "$GOURL.sha256" "$WORK/go.sha256" || die "cannot download the checksum of $GOFILE"
want="$(awk '{ print $1; exit }' "$WORK/go.sha256")"
got="$(sha256sum "$WORK/$GOFILE" | awk '{ print $1 }')"
[ -n "$want" ] && [ "$want" = "$got" ] || die "checksum mismatch for $GOFILE"
rm -rf "$PREFIX/go"
tar -xzf "$WORK/$GOFILE" -C "$PREFIX"
ln -sf "$PREFIX/go/bin/go" "$PREFIX/bin/go"
ln -sf "$PREFIX/go/bin/gofmt" "$PREFIX/bin/gofmt"
say "installed Go $GOVER"

# Node.js
if is_musl; then
	pm_install nodejs npm || die "nodejs and npm could not be installed"
else
	base="https://nodejs.org/dist/latest-v${NODE_MAJOR}.x"
	fetch "$base/SHASUMS256.txt" "$WORK/node.sums" || die "cannot read $base/SHASUMS256.txt: is $NODE_MAJOR a Node.js major version?"
	entry="$(awk -v re="^node-v[0-9.]+-linux-${NODEARCH}\\.tar\\.gz$" '$2 ~ re { print $1 " " $2; exit }' "$WORK/node.sums")"
	[ -n "$entry" ] || die "no linux-$NODEARCH archive in $base/SHASUMS256.txt"
	nodesum="${entry% *}"
	nodefile="${entry#* }"
	fetch "$base/$nodefile" "$WORK/$nodefile" || die "cannot download $base/$nodefile"
	got="$(sha256sum "$WORK/$nodefile" | awk '{ print $1 }')"
	[ "$nodesum" = "$got" ] || die "checksum mismatch for $nodefile"
	rm -rf "$PREFIX/node"
	mkdir -p "$PREFIX/node"
	tar -xzf "$WORK/$nodefile" -C "$PREFIX/node" --strip-components=1
	for bin in node npm npx; do
		ln -sf "$PREFIX/node/bin/$bin" "$PREFIX/bin/$bin"
	done
fi
say "installed Node.js $(node --version 2>/dev/null || printf '%s' "$NODE_MAJOR")"

case ":$PATH:" in
*":$PREFIX/bin:"*) ;;
*) say "add $PREFIX/bin to PATH: export PATH=\"$PREFIX/bin:\$PATH\"" ;;
esac
say "done: run make build"
