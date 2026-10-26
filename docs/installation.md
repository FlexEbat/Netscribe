# Installation

**English** · [Русский](installation.ru.md)

Netscribe runs on any Linux distribution on amd64 or arm64. The release binary is static:
it needs no libraries, no Go and no Node.js on the host, and it has no packages to install
to run. Nmap, Chromium and Docker are optional and each switches on one feature.

| Method | Use it when |
| --- | --- |
| [Guided installer](#guided-installer) | You want the checks, the recommendations and the first account in one run (recommended) |
| [Install script](#install-script) | You want a service user, a config and a systemd unit in one step, and edit the config yourself |
| [Release binary by hand](#release-binary-by-hand) | You manage users, paths and the init system yourself |
| [Docker](#docker) | The host runs containers already |
| [Build from source](#build-from-source) | You develop Netscribe or need an unreleased commit |

## Guided installer

`get.sh` installs the `netscribe` command and runs `netscribe setup`, which does the rest
and explains each step. There are two ways to use it:

- **Without options** it is a guided session in the terminal. It asks the few questions that
  matter, marks problems and says how to fix them. When it ends, `netscribe` is a command
  of its own: typed alone at a terminal it opens the [console](#the-console).
- **With options** (`--yes`, `--admin`, `--listen` and the others below) it asks nothing and
  can run from a provisioning script.

In the guided session every change asks first: creating the account, the directories, the
config, each unit file. Enter takes the answer in brackets, `n` skips the change and stops the
setup before it, and `q` (or `quit`, `exit`, Ctrl-D) leaves at any question. The setup says at
which step it stopped. What was done stays done, and running it again continues from there.
The password of the first administrator is typed into `netscribe user add` itself, which
reads it without echo; Ctrl-C leaves that prompt.

Run it on the host that will scan the network, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/FlexEbat/Netscribe/main/scripts/get.sh -o get.sh
sudo sh get.sh
```

`--version`, `--prefix` and `--base-url` go to `install.sh`, which `get.sh` uses to download
the release and check its checksum. Every other option goes to `netscribe setup`. Piped from
`curl`, the questions are read from the terminal.

The setup walks through nine steps. Each step prints what it found, marks a problem with
`!` or `✗` and says how to fix it:

| Step | What it checks and does |
| --- | --- |
| System | Linux and the architecture, root, systemd, free space for the data |
| The netscribe command | Copies the binary to `/usr/local/bin/netscribe` when it runs from elsewhere |
| Service account | Creates the `netscribe` account and the directories `/etc/netscribe` and `/var/lib/netscribe` with mode 0750 |
| Features | Offers to install Nmap and Chromium with the package manager, and finds the Docker socket |
| Configuration | Proposes the private networks of the host as scan targets, asks [where the panel listens](#where-the-panel-listens) and the scan interval, and writes `netscribe.yaml` with mode 0600. An existing config is never overwritten |
| ICMP probes | Reads `net.ipv4.ping_group_range`. When ICMP echo is off for every group, it writes `/etc/sysctl.d/99-netscribe.conf` for the service group |
| First administrator | Runs `netscribe user add` as the service account when no account exists, so that the database belongs to the service |
| Service | Installs the unit, adds a drop-in for the Docker group and for Nmap, starts the service and waits until the panel answers |
| Result | Runs every check again, lists what is left with the fix for each, and offers to open the console |

The setup proposes networks it finds on the interfaces. A network wider than a /24 is narrowed
to the /24 of the address, and the interfaces of containers and virtual machines are left
out. It writes only the keys it asked about. Every other key keeps its default, and
`netscribe.example.yaml` lists them all.

Options of `netscribe setup`:

| Option | Meaning |
| --- | --- |
| `--dry-run` | Print the plan and the config that would be written. Change nothing |
| `--yes` | Take the recommended answer to every question. Needed when no terminal is attached |
| `--admin NAME` | Create the first administrator with this name. Add `--password-stdin` to read the password from standard input |
| `--targets LIST` | Scan targets of a new config, CIDRs separated by commas |
| `--listen ADDR` | Listen address of a new config, default `127.0.0.1:8080` |
| `--tls-cert FILE`, `--tls-key FILE` | The panel serves TLS with this certificate and key. Needed with a listen address beyond loopback, unless a proxy or `--plain-http` is given |
| `--trusted-proxy LIST` | Address or network of the reverse proxy in front of the panel, separated by commas |
| `--plain-http` | Accept a listen address beyond loopback without TLS or a proxy |
| `--no-service` | Install no unit and start nothing |
| `--config PATH` | Config file to write or keep, default `/etc/netscribe/netscribe.yaml` |

An unattended install:

```sh
printf '%s\n' "$ADMIN_PASSWORD" | sudo sh get.sh --yes --admin alice --password-stdin --targets 192.168.1.0/24
```

### Where the panel listens

The guided setup offers four places, and the same list appears in `netscribe config`:

| Choice | Meaning |
| --- | --- |
| `127.0.0.1:8080` | This host only. It is the recommended default: reach the panel with `ssh -L 8080:127.0.0.1:8080 user@host`, or put a TLS reverse proxy on the same host in front of it |
| An address of an interface, such as `192.168.1.20:8080` | Open to that network only. One entry for each interface the setup finds, container bridges left out |
| `0.0.0.0:8080` | Open to every network of the host |
| Another address | Any `host:port` that the host can bind |

An address beyond loopback sends passwords and session cookies over the network, so the setup
then asks how the traffic is protected: a TLS reverse proxy on another host (its address goes
into `trustedProxies`), a certificate and a key for the panel itself (checked to belong
together, written to `tls`), or plain HTTP after an explicit yes. In a script, the options
`--tls-cert` and `--tls-key`, `--trusted-proxy` and `--plain-http` give the same answers.

### The console

Once the install is done, `netscribe` typed alone at a terminal opens a menu that covers the
whole program. Run it with `sudo`: the config and the database belong to the service account.

| Item | What it does |
| --- | --- |
| Watch | The live view below |
| Check | The checks of `netscribe doctor` |
| Scan | Scan the networks of the config, the same with Nmap, or networks you name. The scan runs as the service account and shows its progress; Ctrl-C ends the scan, not the console |
| Devices | The devices found so far |
| Export | Markdown, HTML, PDF or Draw.io, written to the file you name. The file belongs to you, not to root. An existing file is replaced only after you say yes |
| Settings | Every setting of the config with its value and advice, the SSH hosts, the file in your editor, and a restore of the previous config |
| Accounts | List, add, reset a password and disable, as the service account |
| Service | Status, start, stop, restart, reload the config, start at boot, log |
| Setup | The guided setup again. It skips what is done and repairs what is not |
| Commands | Every command with its arguments, for scripts and cron |

Every menu and every question has a way out, and nothing changes without a yes:

- `b` goes back from any submenu that has a Back item. `q`, `quit`, `exit` or Ctrl-D leaves
  the whole console from any question, however deep.
- Starting a scan, writing an export, starting, stopping or restarting the service, adding,
  resetting or disabling an account, saving a setting, editing the SSH hosts, restoring the
  config and running the setup again each ask first and say what they will do. A no changes
  nothing. Reading (status, log, list, devices, check, watch) asks nothing.

### The watcher

`netscribe watch` refreshes a screen every two seconds (`--interval`) until Ctrl-C:

- the service: state, since when, restarts, whether it starts at boot;
- the panel: whether it answers, how fast, and the last 28 answers as a chart;
- the process: CPU, memory, threads and open files;
- the data: size of the database and its log, free disk space;
- the host: load and free memory;
- the config: valid or not, and when it changed;
- the events: what changed since the watcher started, such as a service that went down, a
  panel that stopped answering, a restart, a config that stopped loading, less than 256 MiB of
  disk left, a memory jump. A bad event rings the terminal bell;
- the last lines of the log of the service.

`netscribe watch --once` prints one sample and exits with 1 when the service is not up or the
panel does not answer, so a monitor or a cron job can call it.

### Settings

`netscribe config` opens the editor: panel access, scanning, discovery, Docker, SNMP, history,
sign-in policy, export, SSH and the list of SSH hosts, each with its value, an explanation and
the recommended value. Every change shows the old and the new value and asks before it is
saved. A change is checked against the whole config before it is saved, the old file stays as
`netscribe.yaml.bak`, and the comments of your file are kept. "Restore the previous config"
swaps the file with that `.bak`, so the swap can be undone. The editor says whether the
running service applies the change by itself (keys under `scan`, `snmp`, `ssh`, `docker`,
`history` and `export`) or needs a restart, and offers the restart. Community strings are typed
without echo and never shown again.

For scripts:

```sh
sudo netscribe config show                        # the file, secrets hidden
sudo netscribe config get scan.interval
sudo netscribe config set scan.interval 30m
sudo netscribe config set scan.targets 10.0.0.0/24 10.0.1.0/24
sudo netscribe config edit                        # your editor, checked before it saves
```

### Check an install

`netscribe doctor` runs the same checks without changing anything, prints them by area and
exits with 1 when a check failed. Run it as root: the config belongs to the service
account and is private to it.

```sh
sudo netscribe doctor
```

It reports a config that does not load, a config the service account cannot read, a port
that another program holds, a panel that listens on the network without TLS, scan targets
that match no network of the host, ICMP that the service group may not send, Nmap without
raw-socket rights, a Docker socket that the service cannot use, a stopped service, a panel
that does not answer and a database without an account.

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

## Verify a download

`checksums.txt` shows that a download is complete. Each release also carries a signed
statement of where its archives were built: the repository, the commit and the workflow.
Check an archive against it with the GitHub CLI:

```sh
gh attestation verify netscribe_0.1.2_linux_amd64.tar.gz --repo FlexEbat/Netscribe
```

A release published before this statement existed has none, and the command says so.

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

## Backup

Everything Netscribe keeps is in the SQLite file named in `database` (devices, links,
accounts, audit log), next to the config. SQLite runs in WAL mode, so a copy of the main
file alone can miss the latest writes. Stop the service and copy the file with its `-wal`
and `-shm` neighbors, or make a consistent copy while the service runs, with the `sqlite3`
program:

```sh
sqlite3 /var/lib/netscribe/netscribe.db ".backup /var/backups/netscribe.db"
```

The file holds password hashes and session records: give the copy the mode `0600`.

## Upgrade and uninstall

Run the install script or the guided installer again to upgrade. The config and the database
stay, and the setup skips every step that is already done. Back up the file
named in `database` before a major upgrade. `sudo sh install.sh --uninstall` removes the
binary and the unit and leaves `/etc/netscribe` and `/var/lib/netscribe` for you to delete.
