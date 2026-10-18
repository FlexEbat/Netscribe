# Netscribe

Netscribe scans your network and writes down what it finds. It discovers devices over ARP,
reads their names, interfaces and neighbours over SNMP and SSH, lists the open services and
the Docker containers, and keeps a history of every scan. You browse the result in a web
panel and export it as Markdown, HTML, PDF or a Draw.io diagram.

It runs as one static binary with an embedded web interface and a SQLite database. It makes
no outbound connections except to the targets you configure, and it sends no telemetry.

## Install

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

To build from source you need Go 1.26 and Node 22: run `make build`.

### Docker

The compose file builds the image from a clean clone and starts the service on the host
network, because ARP and port scans must reach your LAN:

```sh
docker compose -f deploy/compose.yaml up --build -d
curl http://127.0.0.1:8080/healthz
```

The service listens on `127.0.0.1:8080` of the host. Every other route answers `503` until
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

The server reloads the file when it changes and on `SIGHUP`. A new `scan.interval`,
`scan.targets`, `snmp`, `ssh`, `docker`, `history` or `export` value applies without a
restart, and a scan that is running keeps the settings it started with. A change to `listen`,
`database`, `auth`, `tls`, `audit`, `discovery`, `trustedProxies` or `allowPublicTargets`
needs a restart, and the log says so. A file that fails validation leaves the running config
in place, and administrators see the error in the panel.

Run a scan by hand with `netscribe scan`, or pass CIDRs as arguments. `netscribe devices`
prints what the database holds.

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

## Develop

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
