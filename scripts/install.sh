#!/bin/sh
# Installs Netscribe from a GitHub release on any Linux distribution.
#
# The binary is static, so the host needs no libraries. The installer itself needs a POSIX
# shell, curl or wget, tar and sha256sum. It installs a missing one with the package
# manager of the host: apt, dnf, yum, zypper, pacman, apk or xbps.
#
#   sudo sh install.sh                  latest release
#   sudo sh install.sh --version v1.2.0
#   sudo sh install.sh --with-nmap --with-chromium
#   sudo sh install.sh --uninstall
set -eu

REPO="FlexEbat/Netscribe"
BASE_URL="${NETSCRIBE_BASE_URL:-https://github.com/$REPO/releases/download}"
API_URL="${NETSCRIBE_API_URL:-https://api.github.com/repos/$REPO/releases/latest}"
LATEST_URL="${NETSCRIBE_LATEST_URL:-https://github.com/$REPO/releases/latest}"
ETC_DIR="${NETSCRIBE_ETC_DIR:-/etc/netscribe}"
STATE_DIR="${NETSCRIBE_STATE_DIR:-/var/lib/netscribe}"
UNIT_DIR="${NETSCRIBE_UNIT_DIR:-/etc/systemd/system}"
SERVICE_USER="netscribe"
PREFIX="/usr/local"
VERSION=""
WITH_SERVICE=1
WITH_NMAP=0
WITH_CHROMIUM=0
UNINSTALL=0
WORK=""
PM_UPDATED=0

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat <<'USAGE'
usage: install.sh [options]

  --version TAG     install this release, for example v1.2.0 (default: the latest)
  --prefix DIR      install the binary to DIR/bin (default: /usr/local)
  --no-service      do not create the service user, the config or the systemd unit
  --with-nmap       also install Nmap for device identification
  --with-chromium   also install Chromium for PDF export
  --base-url URL    download releases from URL/TAG/ instead of GitHub
  --uninstall       remove the binary and the unit, keep the config and the data
  -h, --help        show this text
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
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		VERSION="$2"
		shift
		;;
	--prefix)
		[ $# -ge 2 ] || die "--prefix needs a value"
		PREFIX="$2"
		shift
		;;
	--base-url)
		[ $# -ge 2 ] || die "--base-url needs a value"
		BASE_URL="$2"
		shift
		;;
	--no-service) WITH_SERVICE=0 ;;
	--with-nmap) WITH_NMAP=1 ;;
	--with-chromium) WITH_CHROMIUM=1 ;;
	--uninstall) UNINSTALL=1 ;;
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

[ "$(uname -s)" = "Linux" ] || die "Netscribe runs on Linux only"
[ "$(id -u)" -eq 0 ] || die "run the installer as root, for example: sudo sh install.sh"

# detect_pm prints the name of the package manager of the host, or nothing.
detect_pm() {
	for pm in apt-get dnf yum zypper pacman apk xbps-install; do
		if command -v "$pm" >/dev/null 2>&1; then
			printf '%s' "$pm"
			return 0
		fi
	done
	return 1
}

# pm_install installs packages with the package manager of the host.
pm_install() {
	pm="$(detect_pm)" || return 1
	case "$pm" in
	apt-get)
		if [ "$PM_UPDATED" -eq 0 ]; then
			apt-get update -qq
			PM_UPDATED=1
		fi
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
		;;
	dnf) dnf install -y -q "$@" ;;
	yum) yum install -y -q "$@" ;;
	zypper) zypper --non-interactive --quiet install "$@" ;;
	pacman) pacman -S --noconfirm --needed "$@" ;;
	apk) apk add --no-cache --quiet "$@" ;;
	xbps-install) xbps-install -Sy "$@" ;;
	esac
}

# ensure_tool makes sure a command exists. The second argument names the packages that
# provide it.
ensure_tool() {
	command -v "$1" >/dev/null 2>&1 && return 0
	say "installing $1"
	# shellcheck disable=SC2086 # the package list is a word list on purpose
	pm_install $2 || die "$1 is missing and could not be installed: install it and run the installer again"
	command -v "$1" >/dev/null 2>&1 || die "$1 is still missing after the install"
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64) printf 'amd64' ;;
	aarch64 | arm64) printf 'arm64' ;;
	*) die "no release for $(uname -m): build from source, see docs/installation.md" ;;
	esac
}

# fetch downloads URL to the file DEST.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 -o "$2" "$1"
	else
		wget -q -O "$2" "$1"
	fi
}

# redirect_tag reads the tag from the redirect of the "latest release" page. Unlike the
# API it has no rate limit for anonymous clients, which matters behind a shared address.
redirect_tag() {
	if command -v curl >/dev/null 2>&1; then
		url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "$LATEST_URL" 2>/dev/null)" || return 0
	else
		url="$(wget -S --spider --max-redirect=0 "$LATEST_URL" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | head -n 1 | tr -d '\r')" || true
	fi
	found="${url##*/}"
	case "$found" in
	v[0-9]*) printf '%s' "$found" ;;
	esac
}

latest_tag() {
	tag=""
	if fetch "$API_URL" "$WORK/latest.json" 2>/dev/null; then
		tag="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$WORK/latest.json" | head -n 1)"
	fi
	if [ -z "$tag" ]; then
		tag="$(redirect_tag)"
	fi
	[ -n "$tag" ] || die "no release found at $API_URL or $LATEST_URL: pass --version TAG, or build from source"
	printf '%s' "$tag"
}

remove_all() {
	if [ -d /run/systemd/system ] && [ "$UNIT_DIR" = "/etc/systemd/system" ]; then
		systemctl disable --now netscribe-helper.path >/dev/null 2>&1 || true
		systemctl disable --now netscribe >/dev/null 2>&1 || true
	fi
	rm -f "$PREFIX/bin/netscribe" "$UNIT_DIR/netscribe.service" "$UNIT_DIR/netscribe-helper.service" "$UNIT_DIR/netscribe-helper.path"
	rm -rf "$UNIT_DIR/netscribe.service.d/20-panel.conf"
	if [ -d /run/systemd/system ] && [ "$UNIT_DIR" = "/etc/systemd/system" ]; then
		systemctl daemon-reload
	fi
	say "removed $PREFIX/bin/netscribe and the unit"
	say "kept $ETC_DIR and $STATE_DIR, delete them by hand to remove the config and the data"
}

create_user() {
	if id "$SERVICE_USER" >/dev/null 2>&1; then
		return 0
	fi
	nologin="$(command -v nologin 2>/dev/null || printf '/usr/sbin/nologin')"
	if command -v useradd >/dev/null 2>&1; then
		useradd --system --user-group --home-dir "$STATE_DIR" --no-create-home --shell "$nologin" "$SERVICE_USER"
	elif command -v adduser >/dev/null 2>&1; then
		addgroup -S "$SERVICE_USER"
		adduser -S -D -H -h "$STATE_DIR" -s "$nologin" -G "$SERVICE_USER" "$SERVICE_USER"
	else
		die "neither useradd nor adduser found: create the system user $SERVICE_USER by hand and run the installer again"
	fi
}

install_nmap() {
	say "installing nmap"
	pm_install nmap || warn "nmap could not be installed, install it with your package manager"
}

install_chromium() {
	say "installing chromium"
	# Debian calls it chromium, Ubuntu ships chromium-browser, the rest use chromium.
	pm_install chromium || pm_install chromium-browser || warn "chromium could not be installed, install it with your package manager"
}

if [ "$UNINSTALL" -eq 1 ]; then
	remove_all
	exit 0
fi

ensure_tool tar "tar"
ensure_tool sha256sum "coreutils"
if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
	ensure_tool curl "curl ca-certificates"
fi

WORK="$(mktemp -d)"
ARCH="$(detect_arch)"
TAG="${VERSION:-$(latest_tag)}"
NUM="${TAG#v}"
ARCHIVE="netscribe_${NUM}_linux_${ARCH}.tar.gz"

say "installing Netscribe $TAG for linux/$ARCH"
fetch "$BASE_URL/$TAG/$ARCHIVE" "$WORK/$ARCHIVE" || die "cannot download $BASE_URL/$TAG/$ARCHIVE"
fetch "$BASE_URL/$TAG/checksums.txt" "$WORK/checksums.txt" || die "cannot download checksums.txt of $TAG"

want="$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$WORK/checksums.txt" | head -n 1)"
[ -n "$want" ] || die "checksums.txt has no entry for $ARCHIVE"
got="$(sha256sum "$WORK/$ARCHIVE" | awk '{ print $1 }')"
[ "$want" = "$got" ] || die "checksum mismatch for $ARCHIVE: expected $want, got $got"

mkdir "$WORK/unpacked"
tar -xzf "$WORK/$ARCHIVE" -C "$WORK/unpacked"
[ -f "$WORK/unpacked/netscribe" ] || die "the archive has no netscribe binary"

install -d -m 0755 "$PREFIX/bin"
install -m 0755 "$WORK/unpacked/netscribe" "$PREFIX/bin/netscribe"
say "installed $PREFIX/bin/netscribe"

if [ "$WITH_SERVICE" -eq 1 ]; then
	create_user
	install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_USER" "$STATE_DIR"
	install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_USER" "$ETC_DIR"

	example="$(find "$WORK/unpacked" -name netscribe.example.yaml | head -n 1)"
	[ -n "$example" ] || die "the archive has no netscribe.example.yaml"
	install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_USER" "$example" "$ETC_DIR/netscribe.example.yaml"
	if [ -e "$ETC_DIR/netscribe.yaml" ]; then
		say "kept $ETC_DIR/netscribe.yaml"
	else
		sed "s#^database:.*#database: $STATE_DIR/netscribe.db#" "$example" >"$WORK/netscribe.yaml"
		install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_USER" "$WORK/netscribe.yaml" "$ETC_DIR/netscribe.yaml"
		say "created $ETC_DIR/netscribe.yaml"
	fi

	unit="$(find "$WORK/unpacked" -name netscribe.service | head -n 1)"
	if [ -d /run/systemd/system ] && [ -n "$unit" ]; then
		install -d -m 0755 "$UNIT_DIR"
		install -m 0644 "$unit" "$UNIT_DIR/netscribe.service"
		if [ "$UNIT_DIR" = "/etc/systemd/system" ]; then
			systemctl daemon-reload
			# An upgrade restarts a running service and leaves a stopped one alone.
			systemctl try-restart netscribe >/dev/null 2>&1 || true
		fi
		say "installed $UNIT_DIR/netscribe.service"
		# The program writes the units of its own version: the helper that lets the panel
		# update and restart the service, and the right to save settings. A failure here
		# only means the panel shows the command to run instead of a button.
		if [ "$UNIT_DIR" = "/etc/systemd/system" ]; then
			"$PREFIX/bin/netscribe" update --units --config "$ETC_DIR/netscribe.yaml" ||
				say "the panel helper was not installed: run sudo netscribe update --units"
		fi
	fi
fi

if [ "$WITH_NMAP" -eq 1 ]; then
	install_nmap
fi
if [ "$WITH_CHROMIUM" -eq 1 ]; then
	install_chromium
fi

# The guided installer (get.sh) runs "netscribe setup" next, which says what to do.
if [ -z "${NETSCRIBE_GUIDED:-}" ]; then
	say ""
	say "Next steps:"
	if [ "$WITH_SERVICE" -eq 1 ]; then
		say "  1. Set scan.targets in $ETC_DIR/netscribe.yaml:"
		say "       sudo -u $SERVICE_USER \${EDITOR:-vi} $ETC_DIR/netscribe.yaml"
		say "  2. Create the first administrator:"
		say "       sudo -u $SERVICE_USER $PREFIX/bin/netscribe user add alice --role admin --config $ETC_DIR/netscribe.yaml"
		if [ -d /run/systemd/system ]; then
			say "  3. Start the panel:"
			say "       sudo systemctl enable --now netscribe"
		else
			say "  3. Start the panel with your init system, as user $SERVICE_USER:"
			say "       $PREFIX/bin/netscribe serve --config $ETC_DIR/netscribe.yaml"
		fi
	else
		say "  cp netscribe.example.yaml netscribe.yaml && chmod 600 netscribe.yaml"
		say "  $PREFIX/bin/netscribe user add alice --role admin"
		say "  $PREFIX/bin/netscribe serve"
	fi
	say "The panel listens on 127.0.0.1:8080. See docs/installation.md for TLS, a reverse proxy and Nmap."
fi
