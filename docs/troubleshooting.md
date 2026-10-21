# Troubleshooting

## The port is already in use

At start the server takes its port before anything else. When another program holds it,
the server logs `port is already in use` and then does one of these:

- On a terminal, it asks: enter another port, type `auto` for the next free port, or press
  Enter to quit.
- With `--auto-port`, it takes the next free port above the busy one without asking and
  logs which one it chose. It tries 100 ports.
- Without a terminal (a service, a container) and without `--auto-port`, it exits with an
  error that names the address.

To choose the port yourself, set `listen` in `netscribe.yaml` or pass `--listen 127.0.0.1:9090`
to `netscribe serve`. The flag wins over the file. To find who holds a port:

```sh
sudo ss -ltnp 'sport = :8080'
```

A port chosen at start is not written to the config. The log line `listening` shows the
address in use. With a reverse proxy or a firewall rule, set the port in `listen` so that it
stays the same.

## The server does not start

| Message | Cause and fix |
| --- | --- |
| `config file permissions 0644 are wider than 0600` | Run `chmod 600 netscribe.yaml`, or pass `--allow-loose-permissions` when the file system cannot hold the mode, as with some bind mounts. |
| `listen: listen must be host:port with a port from 1 to 65535` | The value needs a colon and a port, for example `127.0.0.1:8080`. |
| `parse config: ... field ... not found` | A key is misspelled. Unknown keys are errors on purpose. |
| `load tls certificate` | `tls.certFile` and `tls.keyFile` do not match or cannot be read by the user of the service. |
| `no users: run "netscribe user add"` (HTTP 503) | Create the first administrator, see the README. |

## A scan shows as running after a restart

A scan that the server did not finish, because the process stopped, stays `running` in an
older version. Current versions close such scans as failed when the server starts and log
`closed scans that an earlier run left unfinished`.

## A feature shows no data

| Symptom | Fix |
| --- | --- |
| No devices | Check `scan.targets`. Targets outside the private ranges need `allowPublicTargets: true`, and a target cannot be wider than /16. The scanner reads ARP for the local segment and probes other targets with ICMP and TCP. |
| `ICMP probes are off, TCP probes only` | Informational. Allow the group of the service in `net.ipv4.ping_group_range` to get ICMP. |
| `dnet: Failed to open device` in the log | Nmap lacks raw socket rights. See [Optional packages](installation.md#optional-packages). |
| PDF export fails | Install Chromium or set `export.chromePath`. The Docker image has none. |
| No Docker containers | The user of the service needs access to `docker.socket`. Under systemd, name the group in `SupplementaryGroups=`. |
| A host over SSH is skipped | The host is missing from `ssh.knownHosts`, or its key changed. Add the key with `ssh-keyscan`. |
| SNMP shows nothing | Check the community, the ACL on the device and `snmp.timeout`. |

## The config does not reload

The log and the panel (for administrators) show why. A file that fails validation leaves
the running config in place. Changes to `listen`, `database`, `auth`, `tls`, `audit`,
`discovery`, `trustedProxies` and `allowPublicTargets` need a restart, and the log says so.

When you edit the file as root, the owner can change. The service user must own it, or be
able to read it with mode `0600`.

## Sessions end while a page is open

The panel keeps an event stream open. The server checks the credential of the stream every
25 seconds: a logout, a disabled account, a changed password or a revoked token ends it. The stream does not extend the idle timeout of a session.
