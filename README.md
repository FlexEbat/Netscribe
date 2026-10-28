# Netscribe

**English** · [Русский](README.ru.md)

Netscribe scans your network and writes down what it finds. It discovers devices over ARP,
reads their names, interfaces and neighbours over SNMP and SSH, lists open services and
Docker containers, and keeps a history of every scan. You browse the result in a web panel
and export it as Markdown, HTML, PDF or a Draw.io diagram.

It is one static binary with an embedded web interface and a SQLite database. It sends
packets only to the targets you configure and sends no telemetry. Linux only (amd64, arm64).

## Contents

- [What it does](#what-it-does)
- [Quick start](#quick-start)
- [The `netscribe` command](#the-netscribe-command)
- [Install](#install)
- [First user](#first-user)
- [Configuration](#configuration)
- [Updates and settings in the panel](#updates-and-settings-in-the-panel)
- [Identify devices with Nmap](#identify-devices-with-nmap)
- [Export](#export)
- [TLS and reverse proxy](#tls-and-reverse-proxy)
- [Security](#security)
- [Documentation](#documentation)
- [Develop](#develop)

## What it does

| You get | How |
| --- | --- |
| An inventory of devices, addresses, links, services and containers | ARP, ICMP and TCP probes, SNMP v2c, LLDP, router tables, HTTP banners, mDNS, SSDP, SSH, Docker, optional Nmap |
| A topology you can read and edit | A layered map in the panel; add devices and links by hand when nothing can find them |
| What changed since the last scan | A change feed and a scan history, kept per scan |
| Documentation you can hand over | Markdown, HTML, PDF and Draw.io export, from the panel or the command line |
| A panel for a team | Sign-in, three roles (`viewer`, `operator`, `admin`), access tokens, an audit log |

## Quick start

On the host that will scan the network, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/FlexEbat/Netscribe/main/scripts/get.sh -o get.sh
sudo sh get.sh
```

The guided setup checks the host, proposes the networks to scan, writes the config, starts
the service and creates the first administrator. Each change asks first, and `q` leaves at
any question. When it ends, open `http://127.0.0.1:8080` and sign in. From now on the command
`netscribe` typed alone opens the [console](#the-netscribe-command).

## The `netscribe` command

At a terminal, `sudo netscribe` opens a menu that covers the whole program:

| Item | What it does |
| --- | --- |
| Watch | Live view of the service, the panel and the host, with the events since you started |
| Check | Every check of `netscribe doctor`, with the fix for each finding |
| Scan | Scan the networks of the config, with Nmap, or networks you name |
| Devices | The devices found so far |
| Export | Write the documentation as Markdown, HTML, PDF or Draw.io |
| Settings | Every setting of the config, SSH hosts, restore of the previous config, your editor |
| Accounts | List, add, reset a password, disable |
| Service | Status, start, stop, restart, reload, start at boot, log |
| Setup | The guided setup again; it skips what is done and repairs what is not |
| Commands | Every command, for scripts and cron |

Two rules hold in every menu and every question:

- **A way out.** `b` goes back from any submenu that has a Back item, and `q` (or `quit`,
  `exit`, Ctrl-D) leaves the program from any question.
- **Nothing changes without a yes.** Every action that changes the system, the config, an
  account or the service asks first and names what it will do. A no changes nothing.

Everything the console does is also a command, so it works in scripts:

| Command | Purpose |
| --- | --- |
| `netscribe serve` | Run the web panel |
| `netscribe scan [--nmap] [CIDR...]` | Scan the networks of the config, or the CIDRs given |
| `netscribe devices` | Print the devices found so far |
| `netscribe export --format md\|html\|pdf\|drawio --out FILE` | Write the documentation |
| `netscribe user add\|list\|disable\|passwd NAME` | Manage accounts |
| `netscribe setup` | Guided install; with `--yes`, `--admin`, `--listen` and other flags it asks nothing |
| `netscribe config [show\|get KEY\|set KEY VALUE\|edit]` | Read and change the config, checked before it is saved |
| `netscribe watch [--once]` | Live view; `--once` prints one sample and exits with 1 when the service is down |
| `netscribe doctor` | Check an install and say what to fix; exits with 1 on a failure |
| `netscribe version` | Print the version of the build |

Commands that read the config accept `--config FILE`.

## Install

| Method | Use it when |
| --- | --- |
| [Guided installer](#guided-installer) | You want checks, recommendations and the first account in one run (recommended) |
| [Install script](#install-script) | You want a service user, a config and a unit, and edit the config yourself |
| [Binary](#binary) | You manage users, paths and the init system yourself |
| [Docker](#docker) | The host runs containers already |
| [From source](#from-source) | You develop Netscribe or need an unreleased commit |

[docs/installation.md](docs/installation.md) has every option, the optional packages for each
distribution, backup, upgrade and the steps for hosts without systemd.

### Guided installer

`get.sh` installs the `netscribe` command and runs `netscribe setup`. Without options it is a
guided session. With options it runs unattended, for example:

```sh
printf '%s\n' "$ADMIN_PASSWORD" | sudo sh get.sh --yes --admin alice --password-stdin --targets 192.168.1.0/24
```

`sudo netscribe setup --dry-run` prints what the setup would do and changes nothing.
`sudo netscribe doctor` checks an install later.

### Install script

The classic script installs the release binary, a service user, a config in `/etc/netscribe`
and, with systemd, a unit. You edit the config and create the account yourself:

```sh
curl -fsSL https://raw.githubusercontent.com/FlexEbat/Netscribe/main/scripts/install.sh -o install.sh
sudo sh install.sh
```

### Binary

Download the archive for your platform from the
[releases page](https://github.com/FlexEbat/Netscribe/releases), unpack it and start the panel:

```sh
tar -xzf netscribe_*_linux_amd64.tar.gz
cp netscribe.example.yaml netscribe.yaml && chmod 600 netscribe.yaml
# set scan.targets in netscribe.yaml, then:
./netscribe serve
```

### Docker

The compose file builds the image from a clean clone and starts the service on the host
network, because ARP and port scans must reach your LAN:

```sh
docker compose -f deploy/compose.yaml up --build -d
curl http://127.0.0.1:8080/healthz
```

The service listens on `127.0.0.1:8080` of the host. When another program holds the port,
the server warns and asks for another one on a terminal; `--auto-port` takes the next free
port and `--listen ADDRESS` picks one, see [Troubleshooting](docs/troubleshooting.md#the-port-is-already-in-use).
Every other route answers `503` until you create the first user.

The container runs as user `65532` with all capabilities dropped, a read-only root filesystem
and `no-new-privileges`. The database lives in the `data` volume. The compose file mounts
`netscribe.example.yaml` read-only so the first start works without editing. To use your
own config, set `NETSCRIBE_CONFIG`:

```sh
cp netscribe.example.yaml netscribe.yaml
chmod 600 netscribe.yaml
sudo chown 65532 netscribe.yaml
NETSCRIBE_CONFIG=../netscribe.yaml docker compose -f deploy/compose.yaml up -d
```

The compose file passes `--allow-loose-permissions` because the example file is readable by
all users. Remove the flag from `command` once you use a file with mode `0600`. The image has
no Chromium, so PDF export does not work inside it; print the HTML export to PDF instead.

### From source

You need Go 1.26 and Node 22: run `make build`. On a host that has neither,
`sh scripts/install-build-deps.sh` installs them with `git` and `make`.

## First user

The panel has no default account. The guided setup creates the first administrator. With
another install method, create one with the `user` command:

```sh
./netscribe user add alice --role admin
```

In Docker, run the command inside the container (add `--allow-loose-permissions` while the
container uses the example config):

```sh
docker compose -f deploy/compose.yaml exec netscribe netscribe user add alice --role admin --allow-loose-permissions
```

The password needs at least 12 characters and must not be a common password. Roles:

| Role | Can |
| --- | --- |
| `viewer` | Read the topology |
| `operator` | Read, start scans, edit devices and links |
| `admin` | Everything, including accounts, tokens and the audit log |

## Configuration

`netscribe.example.yaml` lists every key with its default and a comment. Change keys with
`sudo netscribe config` (menu), `sudo netscribe config set KEY VALUE`, or by editing the file.
The keys you set first:

| Key | Meaning |
| --- | --- |
| `listen` | Address of the panel. Default `127.0.0.1:8080` |
| `database` | Path of the SQLite file |
| `scan.targets` | CIDRs to scan, for example `192.168.1.0/24` |
| `scan.interval` | Time between scheduled scans. `0` turns the schedule off, the minimum is `1m` |
| `snmp.communities` | SNMP v2c communities to try. Use a read-only community |
| `ssh.hosts` | Hosts to read over SSH, each with a user and a key file |
| `docker.enabled` | Read containers from the Docker socket |
| `discovery.httpHeaders` | One `HEAD /` to the open web ports (80, 443, 8080), keeping the `Server` header |
| `discovery.mdns` | One mDNS query in each local segment |
| `discovery.ssdp` | One SSDP `M-SEARCH` in each local segment |
| `discovery.nmap` | Ask an installed Nmap about devices nothing else could type. Off by default |
| `discovery.nmapScope` | `untyped` (default) or `all` devices that have an open port |
| `discovery.nmapPath` | Absolute path of `nmap`. Empty means look in `PATH` |

**Reload.** The server reloads the file when it changes and on `SIGHUP`. A new `scan`,
`snmp`, `ssh`, `docker`, `history` or `export` value applies without a restart, and a running
scan keeps the settings it started with. A change to `listen`, `database`, `auth`, `tls`,
`audit`, `discovery`, `trustedProxies` or `allowPublicTargets` needs a restart, and the log
says so. A file that fails validation leaves the running config in place, and administrators
see the error in the panel. `netscribe config` tells you which case a change is.

## Updates and settings in the panel

The running version sits in the bottom-right corner of every page. When a newer release
exists the version glows; a click shows what is new, links to the release and to all
releases, and asks whether to update. Nothing happens before you answer.

- The check asks one fixed GitHub address every few hours and sends nothing about the host.
  Turn it off with `updates.check: false`.
- An administrator can update from the panel on an install made by `netscribe setup` or the
  install script: a small root helper (`netscribe-helper.path`) installs the newest release
  after checking its SHA-256 sum, and restarts the service. Other installs (Docker, or an
  install from before this feature) show the command instead of a button.
- On the server: `sudo netscribe` and choose Update, or `sudo netscribe update` (`--check`
  only looks, `--yes` does not ask).
- **Settings** (administrators) edits the same settings as `netscribe config`, as one checked
  change with a `.bak`. The listen address, TLS, trusted proxies and SSH hosts are changed
  only on the server, so a mistake in the panel cannot lock you out. SNMP communities can be
  replaced but are never shown.

## Identify devices with Nmap

Netscribe types a device from SNMP, LLDP, HTTP, mDNS, SSDP and open ports. A device that none
of these can type stays `unknown`. With [Nmap](https://nmap.org) installed, Netscribe can ask
it about those devices only. Nmap guesses the operating system and the device type from the
way the TCP/IP stack answers, and a guess of at least 85 percent accuracy becomes a kind
through the `nmap` rules in `classify_rules.csv`.

It is off by default because it is loud: for each such device Nmap scans the 100 most common
TCP ports and sends the probes of OS detection. Use it only on networks you own.

- **One scan, command line:** `netscribe scan --nmap`.
- **One scan, panel:** tick "Identify unknown devices with Nmap" next to "Run scan".
- **Every scan,** including scheduled ones: set `discovery.nmap: true`, or turn it on in
  `netscribe config` (the setup also offers to install Nmap).

OS detection needs raw sockets. Under systemd the setup grants them to the service in a
drop-in, which does not widen access for other users. For a binary you start yourself, give
`nmap` the capabilities (every local user can then run raw scans with it, so decide whether
you accept that):

```sh
sudo setcap cap_net_raw,cap_net_admin,cap_net_bind_service+eip "$(command -v nmap)"
```

Without them Nmap fails, Netscribe logs the reason once, skips the stage and finishes the scan.

Nmap runs only for devices with at least one open TCP port. To ask about a single device,
open it in the topology and press "Identify with Nmap": it works for any device, fills in the
system line and sets the kind only while the kind is `unknown` and was not set by hand.
The guess is shown as "System (Nmap)". A device whose Nmap system family is Windows, macOS,
Android or Apple iOS becomes a `host`; Cisco IOS is told apart by the vendor and never
becomes a host; a Linux host is `general purpose` for Nmap, so its kind stays as it was.
A change Nmap makes shows on the Changes page as `Nmap: <device> changed: ...`, filed under
the last completed scan.

**Docker.** The default image has no Nmap. Use the variant and its compose file:

```sh
docker compose -f deploy/compose.nmap.yaml up --build -d
```

It gives the container `NET_RAW`, `NET_ADMIN` and `NET_BIND_SERVICE` back and does not set
`no-new-privileges`, because the kernel ignores file capabilities when that option is on. The
user is still not root and the root filesystem is still read-only.

## Export

```sh
netscribe export --format md --out network.md
netscribe export --format html --out network.html
netscribe export --format drawio --out network.drawio
netscribe export --format pdf --out network.pdf   # needs Chromium or Chrome
```

The panel and the console offer the same formats. PDF export looks for `chromium` and
`google-chrome` in `PATH`; set `export.chromePath` to use another program. In Docker, write
the file into the volume and copy it out:

```sh
docker compose -f deploy/compose.yaml exec netscribe netscribe export --format md --out /data/network.md --allow-loose-permissions
docker compose -f deploy/compose.yaml cp netscribe:/data/network.md .
```

## TLS and reverse proxy

The panel serves plain HTTP unless you set `tls.certFile` and `tls.keyFile`. Beyond the
loopback interface use one of these:

- Set `tls.certFile` and `tls.keyFile` and keep `listen` on the address you need.
- Put a reverse proxy that terminates TLS in front of the panel, keep `listen` on
  `127.0.0.1`, and list the proxy in `trustedProxies` so Netscribe trusts its `X-Forwarded-*`
  headers.

The setup asks which of the two you want when you choose an address beyond loopback.

## Security

Netscribe holds a map of your network: treat its database and config as sensitive.

- **Config file.** It holds SNMP communities and key paths. The service refuses to start when
  the file is readable by other users, unless you pass `--allow-loose-permissions`. Community
  strings are typed without echo and never printed.
- **HTTP, mDNS and SSDP.** Each source is a switch under `discovery`; a source that is off
  sends no packet. The HTTP stage sends `HEAD /` with no credentials, reads at most 8 KiB of
  headers within 2 seconds, never reads a body and never follows a redirect. mDNS and SSDP
  send one packet from each local interface and stay in its segment. Everything these sources
  return is untrusted: it is cut, stripped of control characters and shown as plain text.
- **SNMP v2c** sends the community in clear text. Use a read-only community and limit it by ACL.
- **Scan targets** are limited to RFC 1918 ranges, no wider than /16. `allowPublicTargets`
  lifts the limit; enable it only for networks you own.
- **SSH.** Host keys are checked strictly: a host missing from `ssh.knownHosts`, or one whose
  key changed, is skipped. Use an account that can only read: the scan runs `hostname`,
  `uname -sr`, `ip -o addr` and `ss -H -tln`, nothing else.
- **Docker socket.** Netscribe sends only `GET` requests, but a read-only bind mount does not
  make the Docker API read-only: another process that reaches the socket can start containers
  as root. If you do not need container data, set `docker.enabled: false`. To keep the data
  and narrow the access, put a filtering proxy that allows only `containers`, `networks` and
  `events` in front of the daemon and point `docker.socket` at its unix socket
  ([docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) listens on TCP and
  needs a unix socket bridge).
- **Reports.** Send security reports as described in [SECURITY.md](SECURITY.md).

## Documentation

| Document | English | Русский |
| --- | --- | --- |
| Installation: every method, options, packages, backup, upgrade | [installation.md](docs/installation.md) | [installation.ru.md](docs/installation.ru.md) |
| Architecture: packages, scan stages, request path | [architecture.md](docs/architecture.md) | [architecture.ru.md](docs/architecture.ru.md) |
| Troubleshooting: startup errors, ports, missing data | [troubleshooting.md](docs/troubleshooting.md) | [troubleshooting.ru.md](docs/troubleshooting.ru.md) |

[SECURITY.md](SECURITY.md) says how to report a vulnerability. [tech.md](tech.md) is the
specification the code follows.

## Develop

[CONTRIBUTING.md](CONTRIBUTING.md) explains the setup, the checks and the pull request
process. Coding agents also read [AGENTS.md](AGENTS.md).

```sh
make dev     # Go backend with reload and the Vite dev server
make gate    # lint, typecheck and tests, the same checks CI runs
```

To test a scan against known hosts, start the lab and run the lab test:

```sh
docker compose -f deploy/compose.lab.yaml up --build -d
go test -tags lab ./cmd/netscribe -run Lab
```

## License

MIT, see [LICENSE](LICENSE).
