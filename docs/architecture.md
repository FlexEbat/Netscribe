# Architecture

Netscribe is one process and one binary. It scans the targets of the config, stores what it
finds in SQLite and serves a web panel from the same binary.

```
            +-----------+     +-----------+     +---------+
 targets -> | collector | --> |  scanner  | --> |  store  | <-- api <-- panel (React)
            +-----------+     +-----------+     +---------+       \--> render --> md, html, pdf, drawio
```

## Packages

| Package | Job |
| --- | --- |
| `cmd/netscribe` | The commands `serve`, `scan`, `devices`, `export` and `user`. It wires the packages together and owns startup and shutdown. |
| `internal/config` | Loads and validates the YAML file, holds the `Secret` type and reloads the file on change or `SIGHUP`. |
| `internal/collector` | One collector per source: ARP, probes, SNMP, LLDP, router tables, services, HTTP, mDNS, SSDP, SSH, Docker and Nmap. A collector returns a result and never touches the database. `scanner` runs them and keeps the schedule. |
| `internal/store` | SQLite access: migrations (goose), queries (sqlc), merging a scan result into the database and writing the change feed. |
| `internal/auth` | Argon2id passwords, sessions, access tokens, roles and the login rate limit. |
| `internal/api` | The HTTP routes, the middleware chain (headers, limits, authentication, CSRF, permissions, audit) and the event stream. |
| `internal/render` | Layout and the Markdown, HTML, PDF and Draw.io exports from the stored topology. |
| `internal/events` | The hub that carries scan progress and topology changes to the event streams. |
| `web` | The React panel. The build is embedded in the binary. |

The import rules that keep this shape are in [AGENTS.md](../AGENTS.md).

## A scan

`scanner` allows one scan at a time. A scan runs these stages in order:

1. **Discovery.** ARP finds the hosts of the local segment.
2. **Names.** The system resolver names the hosts. A dead resolver costs one timeout for
   the whole scan.
3. **Enrichment.** In order: probes of routable subnets, SNMP, LLDP, router tables, the
   service port scan, HTTP headers, mDNS and SSDP. An error of one source is logged and the
   scan goes on.
4. **Classification.** Rules in `classify_rules.csv` turn the facts into a device kind. Nmap
   is asked only about devices nothing else could type, and only when it is switched on.
5. **Store.** `ApplyResult` merges the result into the database in one transaction: devices
   match by MAC address, and every difference to the last scan becomes an entry of the
   change feed.
6. **Late collectors.** SSH and Docker add what they read from the hosts you configured.

The scan ends as `done` or `failed`. A failed scan leaves the devices as they were. The
scheduler starts a scan every `scan.interval` and skips a tick while a scan runs.

## Requests

A request passes the middleware in a fixed order: security headers, body limit, the check
that a user exists, authentication (a bearer token, else the session cookie), CSRF for
changes made with a cookie, and the permission of the route. Routes exist only in the route
table of `internal/api/router.go`, each with a permission. Handlers never compare roles.

The event stream (`GET /api/events`) keeps a connection open and sends scan progress and
topology changes. Every 25 seconds it checks the credential again, so a logout, a disabled
account or a revoked token ends the stream.

## Startup and shutdown

`serve` loads the config, binds the listening port, opens the database, closes scans that an
earlier run left unfinished, and only then starts the workers: housekeeping, the Docker
watcher, the scan schedule and the config watcher. The port comes first, so a busy port
stops the command before any worker runs. On `SIGINT` or `SIGTERM` the server stops
accepting requests, waits up to 10 seconds for running ones and cancels the workers through
one context.

## Data

One SQLite file in WAL mode with foreign keys on. The schema lives in
`internal/store/migrations`. Passwords are Argon2id hashes. Session identifiers are stored
as SHA-256 hashes, so a copy of the database does not hold a usable cookie.
