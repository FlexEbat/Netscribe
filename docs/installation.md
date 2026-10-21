# Installation

Netscribe runs on any Linux distribution on amd64 or arm64. The release binary is static:
it needs no libraries, no Go and no Node.js on the host, and it has no packages to install
to run. Nmap, Chromium and Docker are optional and each switches on one feature.

| Method | Use it when |
| --- | --- |
| [Install script](#install-script) | You want a service user, a config and a systemd unit in one step |
| [Release binary by hand](#release-binary-by-hand) | You manage users, paths and the init system yourself |
| [Docker](#docker) | The host runs containers already |
| [Build from source](#build-from-source) | You develop Netscribe or need an unreleased commit |

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/FlexEbat/Netscribe/main/scripts/install.sh -o install.sh
sudo sh install.sh
```

Read the script before you run it. It does this:

1. Finds the architecture and the latest release (`--version v1.2.0` picks another one).
2. Installs `tar`, `sha256sum` and `curl` with the package manager when the host lacks them.
3. Downloads the archive and `checksums.txt` and stops when the checksum does not match.
4. Copies the binary to `/usr/local/bin` (`--prefix DIR` changes the directory).
5. Creates the system user `netscribe`, the directory `/var/lib/netscribe` for the database
   and `/etc/netscribe/netscribe.yaml` with mode `0600`. An existing config stays as it is.
6. On a host that runs systemd, installs `netscribe.service`. A running service restarts
   on an upgrade, a stopped one stays stopped.

Options:

| Option | Effect |
| --- | --- |
| `--with-nmap` | Install Nmap with the package manager, for [device identification](../README.md#identify-devices-with-nmap) |
| `--with-chromium` | Install Chromium, for PDF export |
| `--no-service` | Install only the binary |
| `--base-url URL` | Download from `URL/TAG/` instead of GitHub, for a mirror |
| `--uninstall` | Remove the binary and the unit, keep the config and the data |

The script knows apt, dnf, yum, zypper, pacman, apk and xbps. On another system it still
installs the release when `tar`, `sha256sum` and `curl` or `wget` exist already.

After the script finishes:

```sh
sudo -u netscribe ${EDITOR:-vi} /etc/netscribe/netscribe.yaml      # set scan.targets
sudo -u netscribe /usr/local/bin/netscribe user add alice --role admin --config /etc/netscribe/netscribe.yaml
sudo systemctl enable --now netscribe
```

Edit the config as the user `netscribe`: the server refuses a config file that other users
can read, and a file that root saves with another owner becomes unreadable for the service.
The server reloads the file on change, and `sudo systemctl reload netscribe` forces it.

The unit runs the service with `NoNewPrivileges`, a read-only file system outside
`/var/lib/netscribe` and a restricted set of address families. To read Docker data, name
the group that owns the socket in `SupplementaryGroups=` of the unit.

## Release binary by hand

```sh
tar -xzf netscribe_*_linux_amd64.tar.gz
cp netscribe.example.yaml netscribe.yaml && chmod 600 netscribe.yaml
./netscribe user add alice --role admin
./netscribe serve
```

Check the archive against `checksums.txt` of the release with `sha256sum -c`. On a host
without systemd, start `netscribe serve --config PATH` from your init system as an
unprivileged user, and send `SIGHUP` to reload the config.

## Docker

See [Docker in the README](../README.md#docker). The image has no Chromium and no Nmap.
`deploy/compose.nmap.yaml` builds the variant with Nmap.

## Build from source

You need Go (the version in `go.mod`), Node.js 22, `git` and `make`. A host without them:

```sh
sh scripts/install-build-deps.sh
```

The script installs `git` and `make` with the package manager and takes Go and Node.js from
their official archives, after it checks the checksums, into `/usr/local` as root or into
`~/.local` as a normal user (`--prefix DIR` chooses another place, `--dry-run` prints the
plan). On Alpine and other musl systems the official Node.js build does not run, so the
script installs Node.js from the package manager there.

```sh
make build
./netscribe serve
```

## Optional packages

| Feature | Package | Debian, Ubuntu | Fedora, RHEL | openSUSE | Arch | Alpine | Void |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Nmap identification | nmap | `nmap` | `nmap` | `nmap` | `nmap` | `nmap` | `nmap` |
| PDF export | Chromium | `chromium` (Ubuntu: `chromium-browser`) | `chromium` | `chromium` | `chromium` | `chromium` | `chromium` |

On RHEL and its clones, enable EPEL before you install Chromium.
PDF export looks for `chromium` and `google-chrome` in `PATH`. Set `export.chromePath` to use
another program.

Nmap needs raw sockets for OS detection. Give the service the capabilities in a drop-in
file, which does not widen access for other users the way `setcap` on the binary does:

```ini
# /etc/systemd/system/netscribe.service.d/nmap.conf
[Service]
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN
```

Run `sudo systemctl daemon-reload && sudo systemctl restart netscribe` afterwards.

## Network and firewall

The panel listens on `127.0.0.1:8080` by default. To reach it from another machine, put a
reverse proxy that terminates TLS in front of it, or set `tls.certFile` and `tls.keyFile`,
see [TLS and reverse proxy](../README.md#tls-and-reverse-proxy). Netscribe sends packets
only to the targets you configure.

ICMP probes need the group of the service in `net.ipv4.ping_group_range`. Without it
Netscribe logs that ICMP is off and probes with TCP only. ARP discovery reads
`/proc/net/arp` and needs no raw socket.

When the port is taken, the server warns and asks for another one on a terminal. See
[Troubleshooting](troubleshooting.md#the-port-is-already-in-use).

## Upgrade and uninstall

Run the install script again to upgrade. The config and the database stay. Back up the file
named in `database` before a major upgrade. `sudo sh install.sh --uninstall` removes the
binary and the unit and leaves `/etc/netscribe` and `/var/lib/netscribe` for you to delete.
