# Netscribe

Netscribe scans your network and writes down what it finds. It discovers devices over ARP,
reads their names, interfaces and neighbours over SNMP and SSH, lists the open services and
the Docker containers, and keeps a history of every scan. You browse the result in a web
panel and export it as Markdown, HTML, PDF or a Draw.io diagram.

It runs as one static binary with an embedded web interface and a SQLite database. It makes
no outbound connections except to the targets you configure, and it sends no telemetry.

## Install

### Install script

On any Linux distribution, the script installs the release binary, a service user, a config
in `/etc/netscribe` and, with systemd, a service unit:

```sh
curl -fsSL https://raw.githubusercontent.com/FlexEbat/Netscribe/main/scripts/install.sh -o install.sh
sudo sh install.sh
```

[docs/installation.md](docs/installation.md) lists the options, the optional packages for
each distribution and the steps for hosts without systemd.

### Binary

Download the archive for your platform (linux/amd64 or linux/arm64) from the
[releases page](https://github.com/FlexEbat/Netscribe/releases), unpack it and copy the
example config:

```sh
tar -xzf netscribe_*_linux_amd64.tar.gz
cp netscribe.example.yaml netscribe.yaml
chmod 600 netscribe.yaml
```

Edit `scan.targets` in `netscribe.yaml`, then start the panel:

```sh
./netscribe serve
```

To build from source you need Go 1.26 and Node 22: run `make build`. On a host that has
neither, `sh scripts/install-build-deps.sh` installs them together with `git` and `make`.

### Docker

The compose file builds the image from a clean clone and starts the service on the host
network, because ARP and port scans must reach your LAN:

```sh
docker compose -f deploy/compose.yaml up --build -d
curl http://127.0.0.1:8080/healthz
```

The service listens on `127.0.0.1:8080` of the host. When another program holds the port,
the server warns and asks for another one on a terminal. Pass `--auto-port` to take the next
free port, or `--listen ADDRESS` to choose one, see [Troubleshooting](docs/troubleshooting.md#the-port-is-already-in-use). Every other route answers `503` until
you create the first user.

The container runs as user `65532` with all capabilities dropped, a read-only root
filesystem and `no-new-privileges`. The database lives in the `data` volume. The compose
file mounts `netscribe.example.yaml` read-only so that the first start works without
editing anything. To use your own config, set `NETSCRIBE_CONFIG`:

```sh
cp netscribe.example.yaml netscribe.yaml
chmod 600 netscribe.yaml
sudo chown 65532 netscribe.yaml
NETSCRIBE_CONFIG=../netscribe.yaml docker compose -f deploy/compose.yaml up -d
```

The compose file passes `--allow-loose-permissions` because the example file is readable by
all users. Remove the flag from `command` once you use a file with mode `0600`.

The image has no Chromium, so PDF export does not work inside it. Markdown, HTML and Draw.io
export work, and you can print the HTML export to PDF from a browser.

## Create the first user

The panel has no default account. Create an administrator with the `user` command:

```sh
./netscribe user add alice --role admin
```

In Docker, run the command inside the container. The prompt asks for a password of at least
12 characters. While the container uses the example config, add `--allow-loose-permissions`:

```sh
docker compose -f deploy/compose.yaml exec netscribe netscribe user add alice --role admin --allow-loose-permissions
```

The roles are `viewer` (read), `operator` (read and scan, edit devices) and `admin` (also
manage users, tokens and read the audit log). `netscribe user list`, `disable` and `passwd`
manage the accounts later.

Open `http://127.0.0.1:8080` and sign in.

## Configuration

`netscribe.example.yaml` lists every key with its default and a comment. The ones you set
first:

| Key | Meaning |
| --- | --- |
| `listen` | Address of the panel. Default `127.0.0.1:8080`. |
| `database` | Path of the SQLite file. |
| `scan.targets` | CIDRs to scan, for example `192.168.1.0/24`. |
| `scan.interval` | Time between scheduled scans. `0` turns the schedule off, the minimum is `1m`. |
| `snmp.communities` | SNMP v2c communities to try. Use a read-only community. |
| `ssh.hosts` | Hosts to read over SSH, each with a user and a key file. |
| `docker.enabled` | Read containers from the Docker socket. |
| `discovery.httpHeaders` | Send one `HEAD /` to open web ports (80, 443, 8080) and keep the `Server` header. |
| `discovery.mdns` | Send one mDNS query in each local segment. |
| `discovery.ssdp` | Send one SSDP `M-SEARCH` in each local segment. |
| `discovery.nmap` | Ask an installed Nmap about devices nothing else could type. Off by default. |
| `discovery.nmapScope` | `untyped` asks Nmap only about devices nothing else could type, `all` asks every device that has an open port. |
| `discovery.nmapPath` | Absolute path of `nmap`. Empty means look in `PATH`. |

The server reloads the file when it changes and on `SIGHUP`. A new `scan.interval`,
`scan.targets`, `snmp`, `ssh`, `docker`, `history` or `export` value applies without a
restart, and a scan that is running keeps the settings it started with. A change to `listen`,
`database`, `auth`, `tls`, `audit`, `discovery`, `trustedProxies` or `allowPublicTargets`
needs a restart, and the log says so. A file that fails validation leaves the running config
in place, and administrators see the error in the panel.

Run a scan by hand with `netscribe scan`, or pass CIDRs as arguments. `netscribe devices`
prints what the database holds.

## Identify devices with Nmap

Netscribe types a device from SNMP, LLDP, HTTP, mDNS, SSDP and open ports. A device that
none of these sources can type stays `unknown`. If you have [Nmap](https://nmap.org)
installed, Netscribe can ask it about those devices only. Nmap guesses the operating system
and the device type from the way the TCP/IP stack answers, and Netscribe turns a guess of at
least 85 percent accuracy into a kind with the `nmap` rules in `classify_rules.csv`.

It is off by default, because it is loud: for every such device Nmap runs a TCP port scan of
the 100 most common ports and sends the probes of OS detection. Use it only on networks you
own.

Turn it on for one scan from the command line:

```sh
./netscribe scan --nmap
```

or for one scan from the panel: when Nmap is found, a box "Identify unknown devices with
Nmap" appears next to "Run scan". Or turn it on for every scan, including the scheduled
ones, in `netscribe.yaml`:

```yaml
discovery:
  nmap: true
  nmapPath: "" # empty: look for nmap in PATH
```

OS detection needs raw sockets. Either run Netscribe as root, which this README does not
recommend, or give the `nmap` binary the capabilities it needs. The second choice lets every
local user run raw scans with that binary, so decide whether you accept that:

```sh
sudo setcap cap_net_raw,cap_net_admin,cap_net_bind_service+eip "$(command -v nmap)"
```

Without them Nmap fails, Netscribe logs the reason once, skips the stage and finishes the
scan.

Nmap runs only for devices that have at least one open TCP port, because OS detection needs
an open and a closed port and would otherwise wait for a timeout. To ask about a single
device, open it in the topology and press "Identify with Nmap". That works for any device,
fills in its system line and sets its kind only while the kind is `unknown` and was not set
by hand.

Nmap's guess of the operating system, for example `Linux 5.3 - 5.4`, is stored with the
device and shown in its panel as "System (Nmap)". The guess types a device only when Nmap
names a device type such as `router` or `printer`. A device whose
Nmap system family is Windows, macOS, Android or Apple iOS becomes a `host`. Nmap uses the
name "IOS" for Cisco's system as well, so Netscribe tells Apple's apart by the vendor in
the guess, and a Cisco device is never made a host. A Linux host is `general purpose` for
Nmap and says nothing about its role, so Netscribe leaves its kind alone.

A change that Nmap makes, a new kind or a new system line, shows up on the Changes page
as `Nmap: <device> changed: ...`. It is filed under the last completed scan, because the
feed is kept per scan. With no completed scan yet it is not written.

### Nmap in Docker

The default image has no Nmap. Use the variant, which has Nmap with file capabilities, and
the matching compose file instead of `compose.yaml`:

```sh
docker compose -f deploy/compose.nmap.yaml up --build -d
```

It gives the container `NET_RAW`, `NET_ADMIN` and `NET_BIND_SERVICE` back and does not set
`no-new-privileges`, because the kernel ignores file capabilities when that option is on.
The user is still not root and the root filesystem is still read-only.

## TLS and reverse proxy

The panel serves plain HTTP unless you set `tls.certFile` and `tls.keyFile`. Outside the
loopback interface, use one of these:

- Set `tls.certFile` and `tls.keyFile` and keep `listen` on the address you need.
- Put a reverse proxy that terminates TLS in front of the panel, keep `listen` on
  `127.0.0.1`, and list the proxy address in `trustedProxies` so that Netscribe trusts its
  `X-Forwarded-*` headers.

## Security

Netscribe holds a map of your network, so treat its database and config as sensitive.

- **Config file.** It holds SNMP communities and key paths. The service refuses to start
  when the file is readable by other users, unless you pass `--allow-loose-permissions`.
- **HTTP, mDNS and SSDP.** Each source is a switch under `discovery`, and a source that is
  off sends no packet. The HTTP stage sends `HEAD /` with no credentials or cookies, reads
  at most 8 KiB of headers within 2 seconds, never reads a body and never follows a
  redirect. It skips certificate checks, because it reads headers only. mDNS and SSDP send
  one packet from each local interface and stay in the segment of that interface. Netscribe
  never requests the address in an SSDP `LOCATION` header. Everything these sources return
  is untrusted: it is cut, stripped of control characters and shown as plain text.
- **SNMP v2c** sends the community in clear text. Use a read-only community and limit it by
  ACL on the device.
- **Scan targets** are limited to RFC 1918 ranges, no wider than /16. `allowPublicTargets`
  lifts the limit. Enable it only for networks you own.
- **SSH.** Netscribe checks host keys strictly: a host missing from `ssh.knownHosts`, or one
  whose key changed, is skipped. Use an account that can only read, because the scan runs
  `hostname`, `uname -sr`, `ip -o addr` and `ss -H -tln`.
- **Docker socket.** Netscribe sends only `GET` requests to the daemon, but a read-only
  bind mount does not make the Docker API read-only: any other process that reaches the
  socket can start containers as root. If you do not need container data, set
  `docker.enabled: false` and remove the mount. To keep the data and narrow the access,
  put a filtering proxy in front of the daemon that allows only the `containers`,
  `networks` and `events` endpoints, share its unix socket with Netscribe and point
  `docker.socket` at it. `docker.socket` takes a unix socket path, so a proxy that listens
  on TCP, such as [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy),
  needs a unix socket bridge in front of it.
- **Reports.** Send security reports as described in [SECURITY.md](SECURITY.md).

## Export

Write the documentation to a file with the `export` command:

```sh
./netscribe export --format md --out network.md
./netscribe export --format html --out network.html
./netscribe export --format drawio --out network.drawio
./netscribe export --format pdf --out network.pdf   # needs Chromium or Chrome
```

The panel offers the same formats. PDF export looks for `chromium` and `google-chrome` in
`PATH`. Set `export.chromePath` to use another program.

In Docker, write the file into the volume and copy it out:

```sh
docker compose -f deploy/compose.yaml exec netscribe netscribe export --format md --out /data/network.md --allow-loose-permissions
docker compose -f deploy/compose.yaml cp netscribe:/data/network.md .
```

## Documentation

- [Installation](docs/installation.md): every install method, optional packages, systemd.
- [Architecture](docs/architecture.md): packages, the scan stages, the request path.
- [Troubleshooting](docs/troubleshooting.md): startup errors, ports, missing data.
- [SECURITY.md](SECURITY.md): how to report a vulnerability and what Netscribe protects.

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
