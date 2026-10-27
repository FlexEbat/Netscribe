# tech.md: network documentation generator (Netscribe)

**Version: v0 (draft)** (2026-10-23)

Changelog:
- v0 (amendment 33) - the specification is translated into English (it was in Russian) and the README and the documents in `docs/` got Russian versions (`README.ru.md`, `docs/*.ru.md`). The console and the installer are complete: every menu and question has a way out (`q`, `quit`, `exit`, `b`), every action that changes something asks first, and the console covers scans, devices, export, SSH hosts and the restore of the previous config (slice 22).
- v0 (amendment 32) - slice 23: interface design to the owner's brief. Dark minimalism: a near-black background, matte cards, one electric blue accent, the Geist font (npm packages `@fontsource-variable/geist` and `@fontsource-variable/geist-mono`, OFL license, the font files sit in `dist` and are served from the same origin, the CSP did not change), an animated canvas background of dots and thin lines, blocks that appear one after another, a glow under the cursor on a card. This lifts for slice 23 the slice 17 restriction "animations beyond simple transitions": motion is allowed where it gives feedback, and it is always off under `prefers-reduced-motion`. The default theme is now dark (it was `system`): an unknown or empty value in `localStorage` and in `public/theme-init.js` means `dark`, and the `system` choice stays in the user menu. The light theme is kept and built from the same tokens.
- v0 (amendment 31) - slice 22: the second installer. `scripts/get.sh` installs the `netscribe` command through `install.sh --no-service` and runs `netscribe setup`. `setup` checks the machine, proposes scan targets from the interfaces, writes the config, creates the account and the directories, installs Nmap and Chromium when asked, allows ICMP for the service group, installs the unit and drop-in, starts the service, waits for the panel to answer and creates the first administrator. `netscribe doctor` runs the same checks and changes nothing. Without flags `setup` guides a person through the items in a terminal and at the end offers the console, with flags (`--yes`, `--admin`, `--listen`, `--tls-cert`, `--trusted-proxy` and others) it asks nothing. The bare `netscribe` command in a terminal opens the console: `watch` (the watcher of the service, the panel and the host), the checks, the settings, the accounts, the service. `netscribe config` changes any key of the config with a check, a `.bak` copy and the comments kept. The panel address is chosen from four options (`127.0.0.1`, an interface address, `0.0.0.0`, your own), and for an address beyond loopback the protection is asked: a proxy, TLS or an explicit "no TLS". The classic `install.sh` did not change, except that when it runs from `get.sh` it does not print "Next steps". The new package `internal/setup` imports only `config`, and there are no dependencies in `go.mod`. The unit is embedded as a copy, `internal/setup/netscribe.service`, and a test compares it with `deploy/netscribe.service`: a binary cannot embed a file outside its own packages.
- v0 (amendment 30) - two rare gate failures under load on the runner are fixed. The keep-alive interval of the event stream (`heartbeat`) became a field of the API server, 25 seconds by default, and in tests it is set by the unexported field `Options.heartbeat`: before, a test wrote to a package variable while the stream handlers could read it, and `-race` caught that now and then. In `TestAPDFWaitingForItsTurnGivesUpWithItsContext` the wait "the first print took the slot" was replaced by a signal from the printer instead of `time.Sleep`: on a slow runner the second print could take the slot of the first and the test hung. A rule about package variables and pauses was added to section 10. The causes were found by reading the code, the CI logs were not available.
- v0 (amendment 29) - the lockout after failed logins is counted per pair (IP, username), not per account. Five failures in a row from one address under one name lock that pair for 15 minutes (even with the right password), the same user from another address signs in freely, so someone who knows only the name cannot lock the owner out. Unknown names are counted the same way. The counters live in memory (after a restart they start again, the per-IP limit stays), and the table is limited to 10,000 entries. A wrong current password in `PUT /api/auth/password` still locks the account itself: it needs a live session. The downside: guessing one name from many addresses is limited only by 5 attempts per address, and the list of common passwords and the 12-character minimum work against it.
- v0 (amendment 28) - a password that equals the service name after digits and signs are dropped (`Netscribe-2026!`) is also rejected as common. The password list is rebuilt by the script `scripts/update-common-passwords.sh`. Frontend tests for `TopologyCanvas`, the graph nodes, `LinkBar`, `PortsDialog` and `DeviceForm`.
- v0 (amendment 27) - two sign-in items are closed. A password must not be in the built-in list of common passwords (`internal/auth/common_passwords.txt`, about 20 thousand entries of 12 characters or more from public leaks, SecLists, MIT), compared without regard to case, the error is `password is too common` (400). Question 8 of section 18 is closed. The per-IP login limit counts only failed attempts: the server counts an attempt before it checks the password (parallel guessing does not get around the limit) and gives it back if the password is right (`Limiter.Refund`). Successful logins neither use up the limit nor reset the failure counter, so honest users behind one address do not get a 429 because of each other. While 10 failures per minute are accumulated from an address, every attempt from it gets 429, including one with the right password.
- v0 (amendment 26) - `Docker.Watch` takes `targets func() []netip.Prefix` instead of a slice and asks for the targets at each connection, `Docker.Retarget()` is added, and `Scanner.SetConfig` calls it when `scan.targets` or `allowPublicTargets` changes. The container host no longer stays the old one after a config reload.
- v0 (amendment 25) - slice 21 is added: code check, port and installation. The flags `serve --listen` and `serve --auto-port`, the check of `listen` in `Validate`, the method `Store.FailRunningScans`, `Service.Recheck` for checking an open event stream, the scripts `scripts/install.sh` and `scripts/install-build-deps.sh`, the unit `deploy/netscribe.service`, and the files `AGENTS.md`, `CONTRIBUTING.md` and `docs/`.
- v0 (amendment 24) - two decisions of amendment 23 are made precise. Apple iOS is told apart from Cisco IOS by the `vendor` attribute of the OS class: the fact `family=apple ios` gives the kind `host`, while Cisco stays `family=ios` and gets no kind. Identifying one device now writes a change to the feed: an entry `device_changed` of the form `Nmap: <device> changed: kind unknown -> router; system - -> Linux 5.4` belongs to the last completed scan, because the feed is kept per scan (`changes.scan_id`). Without a completed scan no entry is made. If the answer changes nothing, no entry is created. The scan feed now also reports a change of the OS string (`system`).
- v0 (amendment 23) - slice 20 is implemented. `family=ios` does not set a kind, because Nmap uses one family name for Apple iOS and Cisco IOS. An empty value of `discovery.nmapScope` counts as `untyped`. The request `POST /api/devices/{id}/identify` takes the same slot as a scan, so during a scan it answers 409. Identifying one device does not write to the change history.
- v0 (amendment 22) - slice 20 is added: refinement of Nmap and of the discovery logic (only devices with an open port, the OS family as a fact, the scope `discovery.nmapScope`, identifying one device from the panel).
- v0 (amendment 21) - slice 19: Nmap is refined (see the section of slice 19). A check against a real Nmap 7.94 showed that the output is parsed as expected, and that without capabilities Nmap ends with `dnet: Failed to open device`, which shows in the log. Nmap puts Linux hosts in `general purpose`, so it does not set a device kind for them, and the main benefit is the OS string.
- v0 (amendment 20) - an optional Nmap source is added (after slice 18, outside the slice numbering). The keys `discovery.nmap` (`false` by default) and `discovery.nmapPath` (empty or an absolute path), and the flag `netscribe scan --nmap` turns it on for one run; scans from the panel and on schedule follow the key. Nmap runs only for devices that none of SNMP, LLDP, HTTP, mDNS and SSDP identified, with fixed arguments (`-O -n -Pn --privileged --top-ports 100 -oX -`) and an address parsed as IPv4, without a shell and with a minimal environment. Only XML is read, and a guess with accuracy below 85 percent is dropped. The classifier gets the field `nmap` (between `ssdp` and `services`), the rules map Nmap types to device kinds, and the type `general purpose` deliberately types nothing. OS detection needs raw sockets, so the feature is unavailable in the Docker image (without Nmap and without capabilities), and the README describes `setcap`. There is no switch in the panel.
- v0 (amendment 19) - slice 18 is merged into `main` (PR #40), this is the last slice of the plan. The HTTP banner is assembled as `Server`, then `X-Powered-By: ...`, `realm: ...` and `CN: ...` joined with `; `, and if there is only `Server`, the banner equals its value. A name from mDNS is written with the suffix `.local`, marked with the source `mdns`, and in the store fills only an empty `hostname`. The same service from the port scanner and from the HTTP stage is merged in the scheduler (`mergeServices`), so that late input does not overwrite the name or the banner. A scan that found no banner clears the old one. The `mdns` rules compare the type for a full match, the other fields for a substring. The UTF-8 check runs before lowercasing: `strings.ToLower` turns bad bytes into U+FFFD and hides them. Multicast leaves from a socket bound to the interface address, and the answer is accepted only from the subnet of that interface. The new keys `discovery.httpHeaders`, `discovery.mdns` and `discovery.ssdp` are on by default and apply after a restart. Migration `0009_service_banner.sql`.
- v0 (amendment 18) - slice 17 is merged into `main` (PR #38). The theme is kept in `localStorage` under the key `netscribe.theme` (`light`, `dark`, any other value means `system`), with a switch in the user menu. The libraries `sonner` and `react-style-singleton` (scroll locking in Radix dialogs) create a `<style>` element at run time, and the CSP `style-src 'self'` forbids it, so a Vite plugin in `web/vite.config.ts` turns that off, the CSS of the toast is included as a file, and the scroll-lock rule lives in `index.css`. The plugin fails the build if the code of these libraries changes. This also fixes the toasts of slice 15, which would have been unstyled under the CSP. Criterion 6 (1280 and 390 pixels) and the absence of a theme flash on reload are checked by hand in a browser. The pages already had loading, empty and error states, and the slice added an error boundary with a retry button around the page.
- v0 (amendment 17) - slice 16 is merged into `main` (PR #33). The config in the container is mounted at `/data/netscribe.yaml` read-only, the working directory of the image is `/data`, so `netscribe user add` inside the container works without the `--config` flags. The file `netscribe.example.yaml` in a clone has mode 0644, so `compose.yaml` runs `serve --allow-loose-permissions`, and `user add` and other commands through `exec` get the same flag while the example is used; your own config is connected through `NETSCRIBE_CONFIG`, a file with mode 0600 and owner uid 65532. The image is based on `distroless/static:nonroot` and has no Chromium, so PDF export is unavailable in it. The Docker client (slice 11) can only do a unix socket, so `docker-socket-proxy` over TCP does not connect without a bridge, and the README describes this. `.dockerignore` and a docker section in `dependabot.yml` are added. The README has no screenshot: it has to be taken from a running panel.
- v0 (amendment 16) - slice 15 is merged into `main` (PR #31). Only `scan`, `snmp`, `ssh`, `docker`, `history` and `export` apply on the fly; a change of `listen`, `database`, `auth`, `tls`, `audit`, `discovery`, `trustedProxies` or `allowPublicTargets` writes a warning to the log and needs a restart, and a file whose old keys fail validation together with the new ones (for example, public targets without `allowPublicTargets`) is rejected as a whole. A new `scan.interval` restarts the scheduler's wait at once, and `0` turns the schedule off. The event `config.reloaded` carries no data, `config.error` carries `{ "error": "..." }`. Audit: the actions `config_reload` and `config_reload_failed`, the user `system`. The Docker watcher gets the scan targets at start and does not see their change on the fly. The status in section 16 is updated.
- v0 (amendment 15) - slice 18 is added to the specification: HTTP headers, mDNS and SSDP. They were forbidden earlier by slice 9 and moved to open question 11. Slice 9 still does not read banners, slice 18 does that after slice 17. The keys `discovery.httpHeaders`, `discovery.mdns` and `discovery.ssdp` and the security rules in section 9.6 are added.
- v0 (amendment 14) - the default ports in `scan.ports` are now `22, 53, 80, 443, 445, 515, 554, 631, 3389, 8080, 9100`: without 515, 554, 631 and 9100 the classification rules of printers and cameras did not fire. The note about this in amendment 13 no longer applies.
- v0 (amendment 13) - slice 9 is merged into `main`. The migration of the slice is called `0006_services.sql`. `GET /api/devices/{id}` answers `{ "device", "interfaces", "services" }`. `collector.Result` got `ServicesPolled`: a service that did not answer again is deleted only for a device whose ports were scanned in this scan. The `services` collector goes last in the chain and passes the ports found to `classify`. The default list `scan.ports` has no 9100, 515, 631 and 554, so the classification rules of printers and cameras fire only when those ports are added to the config. The page `/services` filters by service name, and a service without a name is found by port number.
- v0 (amendment 12) - slice 8 is merged into `main`. The order of collectors after discovery: `probe`, `snmp`, `lldp`, `snmparp`, then `classify`: LLDP has to know the trunk ports before `snmparp` decides which hosts of the FDB to bind to ports. A live host found by `probe` gets the source `probe` (the same rank as `arp`). `collector.Result` got `FDBPolled`: an `fdb` link disappears only if the forwarding table of one of its devices has been read. `snmparp` polls the devices whose `sysServices` has the bridge or router bit. `classify` works on the fields `descr`, `services`, `port`, `vendor` in that order of priority, and an error in `classify_rules.csv` stops the rules from starting instead of silently turning a line off. Open ports are so far known only from `probe` (22, 80, 443): the rules for 9100, 515, 631 and 554 start to fire with slice 9, when the port scan appears. Hosts found by router tables are not polled over SNMP in the same scan.
- v0 (amendment 11) - slice 7 is merged into `main` (PR #21). The migration of the slice is called `0005_interfaces.sql` and creates only `interfaces`: the table `links` appeared in slice 5. `GET /api/devices/{id}` answers `{ "device", "interfaces" }`. `collector.Result` got the field `LinksPolled`: an `lldp` link is deleted only if one of its devices was polled, and a silent device does not lose links. The source of a device is not lowered, the order is `manual`, `ssh`, `snmp`, `arp`. `oui.csv` holds a starter set of known prefixes, and the full IEEE registry goes into the same file without code changes. In `DevicePanel` the tabs are built on `@radix-ui/react-tabs`. If the SNMP and LLDP collectors are connected, a scan has four progress stages instead of three.
- v0 (amendment 10) - slice 6 is merged into `main` (PR #19). A token never opens `users:manage`, `audit:read` and `tokens:manage`: a stolen token cannot create new tokens. Of the `/api/auth/*` routes only `GET /api/auth/me` is open to a token. The rights of a token are the intersection of its scope and the current role of its owner. The messages of slice 6 are added to section 8.
- v0 (amendment 9) - a manual device accepts an optional field `hostname` (up to 255 characters). It is stated explicitly that a manual device with a MAC or IP already taken by another record answers 409. The message `hostname is longer than 255 characters` is added to section 8.
- v0 (amendment 8) - slice 5 is merged into `main` (PR #16). The table `links` is created by the migration of slice 5, not of slice 7, because manual links are needed earlier. Two messages that were not in the list are added to section 8: `port is longer than 64 characters` and `version is required`.
- v0 (amendment 7) - slices 3 and 4 are merged into `main` (PR #13 and #14), and a status row is added to section 16.
- v0 (amendment 6) - slice 4, criterion 3: the guarantee "they do not move" applies to nodes with saved coordinates.
- v0 (amendment 5) - section 15: the "Do not" items apply in their own slice, and links to the slices that implement the deferred work are added in slices 1, 3, 4 and 12. The content of the slices did not change.
- v0 (amendment 4) - the answer of `GET /api/devices` has the form `{ "devices": [...] }`. The login attempt limit is implemented by its own limiter (`internal/auth/ratelimit.go`), and `golang.org/x/time/rate` is not used.
- v0 (amendment 3) - the project is called Netscribe: the working name netdoc is replaced everywhere (module, binary, cookie, variables, texts). Question 1 of section 18 is closed.
- v0 (amendment 2) - section 9 "Security", authentication, roles and multi-user mode, manual device addition and extended discovery are added. Slices 2, 5, 6 and 8 are new, the others moved.
- v0 - a draft for finalization. The contracts are not frozen. Section 18 lists the open questions. After they are answered the version rises to v1 and the contracts are frozen.

---

## 0. How to read this file

This is the source of truth of the project. Read it before every task and obey it to the letter.

- Names of tables, fields, types, functions and routes come from here. Do not invent your own.
- The contract you need is not here → STOP, issue a `CONTRACT GAP` block (section 14). Do not write code with an invented type.
- Only the project owner changes this file. Every change of a contract raises the version at the top of the file.
- Work one slice at a time. Stages and slices are in section 16, go through it from top to bottom.
- While the version is v0, contracts may be edited. From version v1 they are frozen.

---

## 1. Project

A panel for documenting a local network. The service scans the network on a schedule and on a button (ARP, SNMP, LLDP, SSH, Docker), keeps an inventory of devices, IP addresses, links, services and containers, draws the topology and hands out the documentation in Markdown, HTML, PDF and Draw.io. One binary, the web panel is embedded. Devices are found automatically, and what cannot be discovered is added by hand. The panel is multi-user: sign-in by login and password, three roles (`viewer`, `operator`, `admin`), an audit log.

Who it is for: small and medium businesses without network documentation, and students who have to hand it in.

Why: documentation goes stale. The service rebuilds it from the live network and shows what changed since the last scan.

Goal of v1: a working panel with topology, inventory, containers, export and access control that passes the check gate. Not a platform: no multi-tenancy (several organizations in one instance), no alerting, no traffic monitoring and no device management.

Security boundaries (they apply in all slices):

- The service scans only the targets from the config. By default only the private ranges of RFC 1918 are allowed. Public addresses are turned on by the explicit flag `allowPublicTargets`.
- The service only reads. SNMP: GET, GETNEXT, GETBULK, no SET. SSH: a fixed set of read commands. Docker: only list requests and the event stream.
- Access to the panel only after sign-in. Rights are checked on the server for every route. The panel listens on `127.0.0.1` by default. The full list of security parameters is in section 9, and it is mandatory for every slice.
- Secrets (SNMP community, paths to keys) do not get into logs, the API and exports.

---

## 2. Stack

Backend:

- Go (the current stable version, pinned in `go.mod`), in v1 Linux only
- `net/http` + `chi`, logs through `log/slog`
- SQLite through `modernc.org/sqlite` (pure Go, no CGO), `goose` migrations, queries through `sqlc`
- `gosnmp` (SNMP v2c), `golang.org/x/crypto/ssh`, the official Docker Go client, `fsnotify`, `gopkg.in/yaml.v3`
- `golang.org/x/crypto/argon2` (passwords), `crypto/tls` from the standard library
- `chromedp` only for PDF export

Frontend:

- React, TypeScript strict, Vite
- Tailwind CSS, shadcn/ui (components are copied into the repository)
- React Flow (`@xyflow/react`) for the topology, Recharts for charts
- TanStack Query for server state, React Router for routes
- The built frontend is embedded in the binary through `go:embed`

Tests and checks: `go test`, `gofmt`, `go vet`, `golangci-lint` (including `gosec`), `govulncheck`, vitest, eslint (with `jsx-a11y`), prettier, `tsc`, `npm audit`.

Delivery: a native binary (the main way) and a Docker image with `compose.yaml`.

No ORM, no PostgreSQL in v1, no Redux or other state managers, no CSS-in-JS, no GraphQL and gRPC, no message brokers. One process, one binary, one database file. Authentication is our own (users and sessions in SQLite), and there are no external providers in v1.

---

## 3. Folder structure

```
cmd/
  netscribe/
    main.go                   subcommands serve, scan, devices, export, user, setup, doctor
    setup.go                  flags of setup and doctor, the rest is in internal/setup
  seed/
    main.go                   a demo network for development and screenshots, account dev from NETSCRIBE_DEV_PASSWORD
internal/
  config/
    config.go                 config structs, YAML loading, Validate, type Secret
    watch.go                  fsnotify and SIGHUP, atomic swap of the config
  auth/
    password.go               argon2id: hash, check, password rules
    common_passwords.txt      list of common passwords to reject (go:embed)
    session.go                sessions and access tokens: issue, check, revoke
    rbac.go                   roles, permissions, the table "role - permissions"
    ratelimit.go              login attempt limit
  model/
    model.go                  domain types and constants (section 4)
  store/
    store.go                  Open(path), the interface Repo, ErrNotFound
    apply.go                  ApplyResult: merging a scan result into the database, writing changes
    migrations/               goose, files NNNN_<topic>.sql, one migration per slice, numbered in slice order
    queries/                  SQL for sqlc
    db/                       sqlc code, generated, do not edit by hand
  collector/
    collector.go              the interface Collector, Input, Result
    arp.go                    ARP warm-up and reading /proc/net/arp
    snmp.go                   sysName, sysDescr, ifTable
    lldp.go                   neighbors from the LLDP-MIB
    snmparp.go                ARP tables and forwarding tables (FDB) of routers and switches
    probe.go                  ICMP and TCP probing of routable subnets
    classify.go               device kind by rules, the rules are in classify_rules.csv
    oui.go, oui.csv           vendor by MAC (built-in table)
    services.go               TCP connect scan of a list of ports
    httpbanner.go             HTTP(S) headers on open web ports (slice 18)
    mdns.go, ssdp.go          mDNS and SSDP queries in the local segment (slice 18)
    ssh.go                    fixed read commands
    docker.go                 containers, networks, events
    scanner.go                scan orchestration, schedule, writing to store, events
  events/
    hub.go                    event bus for SSE
  setup/
    env.go                    Env: everything the package does to the machine; real and fake
    checks.go                 checks: Diagnose returns []Finding (OK, Info, Warn, Fail)
    setup.go                  the steps of setup and Doctor
    listen.go                 choice of the panel address and its protection (TLS, proxy)
    settings.go, yamledit.go  config editor: groups of settings, get/set/show/edit, YAML with comments
    watch.go                  the watcher: samples, events, screen
    console.go                the console menu
    render.go                 config, unit, drop-in, choosing targets from interfaces
    prompt.go, ui.go          questions and output without external libraries
    netscribe.service         a copy of deploy/netscribe.service
  render/
    layout.go                 layered layout that respects saved coordinates
    svg.go                    SVG diagram
    mermaid.go                Mermaid graph
    markdown.go
    drawio.go
    html.go
    pdf.go
    templates/                html/template
  api/
    router.go                 the route table (method, path, permission, handler), serving the SPA
    middleware.go             security headers, limits, authentication, CSRF, permission check, audit
    auth.go, users.go, tokens.go, audit.go
    devices.go, links.go, scans.go, topology.go, containers.go, services.go, changes.go
    export.go
    sse.go
web/
  embed.go                    go:embed dist
  src/
    main.tsx
    api/
      client.ts               fetch wrapper and response types (a mirror of section 4)
      events.ts               SSE subscription
    components/
      ui/                     shadcn/ui primitives
      DeviceNode.tsx, TopologyCanvas.tsx, DevicePanel.tsx
      ScanBar.tsx, ExportMenu.tsx, StatusBadge.tsx, EmptyState.tsx
      DeviceForm.tsx, RoleBadge.tsx, UserMenu.tsx, RequirePermission.tsx
    pages/
      Login.tsx, Account.tsx, Topology.tsx, Inventory.tsx, Containers.tsx, Services.tsx, Changes.tsx
      Users.tsx, Audit.tsx
    lib/                      pure logic without React (IP sorting, filters, formatting, permissions.ts)
  public/
    theme-init.js             theme choice before the first paint, an external file (there are no inline scripts)
  tests/
deploy/
  Dockerfile
  compose.yaml                the service in production
  compose.lab.yaml            test lab
  lab/                        lab configs
testdata/                     snmpwalk fixtures, /proc/net/arp, golden files
netscribe.example.yaml
Makefile
.air.toml, .golangci.yml, sqlc.yaml
SECURITY.md
```

Placement rules:

- SQL lives only in `internal/store/migrations` and `internal/store/queries`. The code in `internal/store/db` is generated by sqlc.
- The package `collector` does not import `store`. A collector returns a `Result`, and `scanner` writes it through `Repo.ApplyResult`.
- The package `render` does not import `collector` and `api`. It takes `model.Topology` and `[]model.Change`.
- The package `api` contains no SQL and does not call collectors directly. It uses `store.Repo`, `scanner` and `render`.
- The frontend talks to the server only through `web/src/api/`. Components do not call `fetch` themselves.
- Pure logic (parsers, layout, validation, IP sorting) lives in functions without network and database, so that it can be tested without them.
- Values of type `config.Secret` are not logged, not serialized to JSON and do not get into exports.
- Routes are registered only through the table in `internal/api/router.go` (method, path, permission, handler). Direct registration through `mux.Handle` is forbidden. A route without a permission is not registered, and public routes are marked explicitly.
- The package `auth` does not import `api` and `store`: it gets data through the interface `AuthRepo`.
- The package `setup` imports only `config`. Everything it does to the machine (files, processes, network, accounts) goes through `setup.Env`, so tests do not touch `/etc` and do not start processes.
- Only middleware checks permissions. Handlers do not compare roles.
- The frontend hides unavailable actions according to `web/src/lib/permissions.ts`. This is a convenience, and the protection works on the server.

---

## 4. Data contract (draft)

Time is stored in UTC, in SQLite it is TEXT in RFC 3339 format. Column names in the database are `snake_case`, Go fields are `CamelCase`, JSON fields are `camelCase`.

### 4.1 Database schema

The schema is shown in its final form. Each table is created by the migration of the slice that uses it (section 16), and the columns needed later are added by the migrations of the following slices (`ALTER TABLE`). The table `devices` in slice 1 is created without the columns `manual`, `label`, `notes`, `kind_locked`, `version`, `updated_by`.

```sql
CREATE TABLE devices (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	mac           TEXT NOT NULL DEFAULT '',        -- lowercase aa:bb:cc:dd:ee:ff, empty if unknown
	ip            TEXT NOT NULL DEFAULT '',        -- may be empty for a manual device
	hostname      TEXT NOT NULL DEFAULT '',
	vendor        TEXT NOT NULL DEFAULT '',
	kind          TEXT NOT NULL DEFAULT 'unknown', -- router, switch, ap, firewall, server, nas, printer, camera, iot, host, unknown
	description   TEXT NOT NULL DEFAULT '',        -- sysDescr or uname
	source        TEXT NOT NULL,                   -- arp, probe, snmp, ssh, local, manual
	online        INTEGER NOT NULL DEFAULT 1,      -- 0 or 1
	manual        INTEGER NOT NULL DEFAULT 0,      -- 1: created by hand, scans neither delete nor mark it offline
	label         TEXT NOT NULL DEFAULT '',        -- a name from a person, shown instead of hostname
	notes         TEXT NOT NULL DEFAULT '',
	kind_locked   INTEGER NOT NULL DEFAULT 0,      -- 1: the kind was set by a person, scans do not change it
	version       INTEGER NOT NULL DEFAULT 1,      -- grows on every edit by a person, optimistic locking
	updated_by    INTEGER REFERENCES users(id) ON DELETE SET NULL,
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	x             REAL,                            -- canvas coordinates, NULL until moved
	y             REAL
);
CREATE UNIQUE INDEX devices_mac ON devices(mac) WHERE mac <> '';
CREATE UNIQUE INDEX devices_ip_nomac ON devices(ip) WHERE mac = '' AND ip <> '';

CREATE TABLE interfaces (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name      TEXT NOT NULL,
	mac       TEXT NOT NULL DEFAULT '',
	ip        TEXT NOT NULL DEFAULT '',
	up        INTEGER NOT NULL DEFAULT 1,
	UNIQUE (device_id, name, ip)
);

CREATE TABLE links (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	a_device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	a_port       TEXT NOT NULL DEFAULT '',
	b_device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	b_port       TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL,                    -- lldp, fdb, manual
	last_seen_at TEXT NOT NULL,
	CHECK (a_device_id < b_device_id),
	UNIQUE (a_device_id, a_port, b_device_id, b_port)
);

CREATE TABLE services (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	device_id     INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	port          INTEGER NOT NULL,
	proto         TEXT NOT NULL,                   -- tcp, udp
	name          TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at  TEXT NOT NULL,
	UNIQUE (device_id, port, proto)
);

CREATE TABLE containers (
	id              TEXT PRIMARY KEY,              -- full Docker id
	device_id       INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	name            TEXT NOT NULL,
	image           TEXT NOT NULL,
	state           TEXT NOT NULL,                 -- running, exited, paused, restarting, created, dead
	compose_project TEXT NOT NULL DEFAULT '',
	ports           TEXT NOT NULL DEFAULT '[]',    -- JSON, an array of PortMapping
	networks        TEXT NOT NULL DEFAULT '[]',    -- JSON, an array of ContainerNetwork
	first_seen_at   TEXT NOT NULL,
	last_seen_at    TEXT NOT NULL
);

CREATE TABLE scans (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	target          TEXT NOT NULL,                 -- targets separated by commas
	status          TEXT NOT NULL,                 -- running, done, failed
	started_at      TEXT NOT NULL,
	finished_at     TEXT,
	error           TEXT NOT NULL DEFAULT '',
	device_count    INTEGER NOT NULL DEFAULT 0,
	link_count      INTEGER NOT NULL DEFAULT 0,
	container_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE changes (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	scan_id   INTEGER NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
	kind      TEXT NOT NULL,   -- device_added, device_online, device_offline, device_changed,
	                           -- link_added, link_removed, container_added, container_removed
	entity_id TEXT NOT NULL,
	summary   TEXT NOT NULL,
	at        TEXT NOT NULL
);

CREATE TABLE users (
	id                   INTEGER PRIMARY KEY AUTOINCREMENT,
	username             TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name         TEXT NOT NULL DEFAULT '',
	password_hash        TEXT NOT NULL,            -- argon2id, PHC format
	role                 TEXT NOT NULL,            -- viewer, operator, admin
	disabled             INTEGER NOT NULL DEFAULT 0,
	must_change_password INTEGER NOT NULL DEFAULT 0,
	failed_logins        INTEGER NOT NULL DEFAULT 0,
	locked_until         TEXT,
	created_at           TEXT NOT NULL,
	last_login_at        TEXT
);

CREATE TABLE sessions (
	id_hash      TEXT PRIMARY KEY,                 -- SHA-256 of the cookie value, the identifier itself is not stored
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	csrf_token   TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	expires_at   TEXT NOT NULL,                    -- absolute lifetime
	ip           TEXT NOT NULL DEFAULT '',
	user_agent   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE api_tokens (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	prefix       TEXT NOT NULL,                    -- the first 8 characters, for the list
	token_hash   TEXT NOT NULL UNIQUE,             -- SHA-256 of the token
	scope        TEXT NOT NULL,                    -- read, write; not above the role of the owner
	created_at   TEXT NOT NULL,
	expires_at   TEXT NOT NULL,
	last_used_at TEXT,
	revoked      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	at        TEXT NOT NULL,
	user_id   INTEGER,                             -- NULL if the user is unknown
	username  TEXT NOT NULL DEFAULT '',
	action    TEXT NOT NULL,                       -- list in section 9.8
	entity    TEXT NOT NULL DEFAULT '',
	entity_id TEXT NOT NULL DEFAULT '',
	result    TEXT NOT NULL,                       -- ok, denied, error
	ip        TEXT NOT NULL DEFAULT '',
	detail    TEXT NOT NULL DEFAULT ''             -- one line, no secrets
);
```

Device identity: by MAC if it is known, otherwise by IP. A device with the same MAC and a new IP updates the row and does not create a new one. A device that is not in a completed scan gets `online = 0` and stays in the database. A discovered device that matches by IP a record without a MAC (including a manual one) completes that record with the MAC and the scan data and does not create a new row. Devices with `manual = 1` and links with `source = 'manual'` are neither deleted nor marked offline by scans. Fields set by a person (`label`, `notes`, and also `kind` when `kind_locked = 1`) are not overwritten by scans.

### 4.2 Go types (`internal/model/model.go`)

```go
type DeviceKind string

const (
	KindRouter   DeviceKind = "router"
	KindSwitch   DeviceKind = "switch"
	KindAP       DeviceKind = "ap"
	KindFirewall DeviceKind = "firewall"
	KindServer   DeviceKind = "server"
	KindNAS      DeviceKind = "nas"
	KindPrinter  DeviceKind = "printer"
	KindCamera   DeviceKind = "camera"
	KindIoT      DeviceKind = "iot"
	KindHost     DeviceKind = "host"
	KindUnknown  DeviceKind = "unknown"
)

type Device struct {
	ID          int64      `json:"id"`
	MAC         string     `json:"mac"`
	IP          string     `json:"ip"`
	Hostname    string     `json:"hostname"`
	Vendor      string     `json:"vendor"`
	Kind        DeviceKind `json:"kind"`
	Description string     `json:"description"`
	Source      string     `json:"source"`
	Online      bool       `json:"online"`
	Manual      bool       `json:"manual"`
	Label       string     `json:"label"`
	Notes       string     `json:"notes"`
	KindLocked  bool       `json:"kindLocked"`
	Version     int64      `json:"version"`
	FirstSeenAt time.Time  `json:"firstSeenAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	X           *float64   `json:"x"`
	Y           *float64   `json:"y"`
}

type Interface struct {
	ID       int64  `json:"id"`
	DeviceID int64  `json:"deviceId"`
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Up       bool   `json:"up"`
}

type Link struct {
	ID         int64     `json:"id"`
	ADeviceID  int64     `json:"aDeviceId"` // always less than BDeviceID
	APort      string    `json:"aPort"`
	BDeviceID  int64     `json:"bDeviceId"`
	BPort      string    `json:"bPort"`
	Source     string    `json:"source"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type Service struct {
	ID          int64     `json:"id"`
	DeviceID    int64     `json:"deviceId"`
	Port        int       `json:"port"`
	Proto       string    `json:"proto"`
	Name        string    `json:"name"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

type PortMapping struct {
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Proto         string `json:"proto"`
}

type ContainerNetwork struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

type Container struct {
	ID             string             `json:"id"`
	DeviceID       int64              `json:"deviceId"`
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	State          string             `json:"state"`
	ComposeProject string             `json:"composeProject"`
	Ports          []PortMapping      `json:"ports"`
	Networks       []ContainerNetwork `json:"networks"`
	FirstSeenAt    time.Time          `json:"firstSeenAt"`
	LastSeenAt     time.Time          `json:"lastSeenAt"`
}

type Scan struct {
	ID             int64      `json:"id"`
	Target         string     `json:"target"`
	Status         string     `json:"status"` // running, done, failed
	StartedAt      time.Time  `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	Error          string     `json:"error"`
	DeviceCount    int        `json:"deviceCount"`
	LinkCount      int        `json:"linkCount"`
	ContainerCount int        `json:"containerCount"`
}

type Change struct {
	ID       int64     `json:"id"`
	ScanID   int64     `json:"scanId"`
	Kind     string    `json:"kind"`
	EntityID string    `json:"entityId"`
	Summary  string    `json:"summary"`
	At       time.Time `json:"at"`
}

type Topology struct {
	Devices    []Device    `json:"devices"`
	Links      []Link      `json:"links"`
	Containers []Container `json:"containers"`
}
```

The types of users and access (in the same package). The table "role - permissions" lives in `internal/auth/rbac.go` and repeats section 9.3.

```go
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

type Permission string

const (
	PermTopologyRead Permission = "topology:read"
	PermExportRun    Permission = "export:run"
	PermTokensManage Permission = "tokens:manage"
	PermScansRun     Permission = "scans:run"
	PermDevicesWrite Permission = "devices:write"
	PermAuditRead    Permission = "audit:read"
	PermUsersManage  Permission = "users:manage"
)

type User struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"displayName"`
	Role               Role       `json:"role"`
	Disabled           bool       `json:"disabled"`
	MustChangePassword bool       `json:"mustChangePassword"`
	CreatedAt          time.Time  `json:"createdAt"`
	LastLoginAt        *time.Time `json:"lastLoginAt"`
}

// UserRecord lives only inside store and auth and does not go to the API.
type UserRecord struct {
	User
	PasswordHash string     `json:"-"`
	FailedLogins int        `json:"-"`
	LockedUntil  *time.Time `json:"-"`
}

type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"` // read, write
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	Revoked    bool       `json:"revoked"`
}

type AuditEntry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	UserID   *int64    `json:"userId"`
	Username string    `json:"username"`
	Action   string    `json:"action"`
	Entity   string    `json:"entity"`
	EntityID string    `json:"entityId"`
	Result   string    `json:"result"` // ok, denied, error
	IP       string    `json:"ip"`
	Detail   string    `json:"detail"`
}
```

Types of manual input:

```go
type ManualDevice struct {
	MAC, IP, Hostname, Label, Notes string
	Kind                            DeviceKind
}

type DeviceEdit struct { // a nil field is not changed
	Label   *string
	Notes   *string
	Kind    *DeviceKind // a non-empty value sets kind_locked = 1
	Version int64       // the version the editor saw, a required field
}

type ManualLink struct {
	ADeviceID, BDeviceID int64
	APort, BPort         string
}
```

Types of collector input (in the same package). `DeviceKey` refers to a device before it has an `id`: MAC takes priority over IP.

```go
type DeviceKey struct {
	MAC string
	IP  string
}

type DeviceInput struct {
	Key         DeviceKey
	Hostname    string
	Vendor      string
	Description string
	Kind        DeviceKind
	Source      string
}

type InterfaceInput struct {
	Device DeviceKey
	Name   string
	MAC    string
	IP     string
	Up     bool
}

type LinkInput struct {
	A, B         DeviceKey
	APort, BPort string
	Source       string
}

type ServiceInput struct {
	Device DeviceKey
	Port   int
	Proto  string
	Name   string
}

type ContainerInput struct {
	Device         DeviceKey
	ID, Name       string
	Image, State   string
	ComposeProject string
	Ports          []PortMapping
	Networks       []ContainerNetwork
}
```

A device has no fields `owner`, `location` and `vlan` in v0. If you need them - `CONTRACT GAP`.

### 4.3 Config (YAML)

The type `config.Secret` replaces the value with `***` when printed through `String()`, `MarshalJSON` and `slog`.

```yaml
listen: 127.0.0.1:8080
database: ./netscribe.db
allowPublicTargets: false

scan:
  targets: [192.168.1.0/24]
  interval: 15m                  # 0 turns the schedule off
  ports: [22, 53, 80, 443, 445, 515, 554, 631, 3389, 8080, 9100]   # an empty list turns the port scan off

snmp:
  communities: [public]          # Secret
  timeout: 2s

ssh:
  knownHosts: ~/.ssh/known_hosts
  hosts:
    - address: 192.168.1.10
      user: audit
      keyFile: ~/.ssh/netscribe_ed25519    # keys only, no passwords

docker:
  enabled: true
  socket: /var/run/docker.sock

history:
  keepScans: 200

audit:
  keepDays: 365

auth:
  sessionIdleTimeout: 8h         # from 15m to 24h
  sessionMaxAge: 168h            # not less than sessionIdleTimeout, not more than 720h
  minPasswordLength: 12          # not less than 12
  maxFailedLogins: 5             # from 3 to 20
  lockoutDuration: 15m
  argon2:
    memoryKiB: 65536             # not less than 19456
    iterations: 3                # not less than 2
    parallelism: 2               # not less than 1

tls:
  certFile: ""                   # certFile and keyFile are set together or not at all
  keyFile: ""

trustedProxies: []               # CIDRs of reverse proxies whose X-Forwarded-* are accepted

discovery:
  routerTables: true             # ARP and FDB over SNMP from known routers and switches
  httpHeaders: true              # slice 18: HTTP(S) headers on open web ports
  mdns: true                     # slice 18: one mDNS query in the local segment
  ssdp: true                     # slice 18: one SSDP M-SEARCH query in the local segment
  nmap: false                    # optional: nmap -O for devices that nothing else identified
  nmapPath: ""                   # empty: look for nmap in PATH, otherwise an absolute path
  probeRoutedTargets: false      # ICMP and TCP probing of subnets behind a router

export:
  chromePath: ""                 # empty: look for chromium and google-chrome in PATH
```

---

## 5. Contracts of data access and of collectors (draft)

### 5.1 `store.Repo`

`internal/store/store.go` exports exactly this:

```go
type Repo interface {
	ListDevices(ctx context.Context, f DeviceFilter) ([]model.Device, error)
	GetDevice(ctx context.Context, id int64) (model.Device, error)            // ErrNotFound
	SetDevicePosition(ctx context.Context, id int64, x, y float64) error      // ErrNotFound
	ListInterfaces(ctx context.Context, deviceID int64) ([]model.Interface, error)
	ListLinks(ctx context.Context) ([]model.Link, error)
	ListServices(ctx context.Context, deviceID int64) ([]model.Service, error) // 0 means all devices
	ListContainers(ctx context.Context) ([]model.Container, error)
	Topology(ctx context.Context) (model.Topology, error)                      // one read transaction

	CreateDevice(ctx context.Context, in model.ManualDevice, userID int64) (model.Device, error)         // ErrConflict
	UpdateDevice(ctx context.Context, id int64, e model.DeviceEdit, userID int64) (model.Device, error)  // ErrNotFound, ErrVersionConflict
	DeleteDevice(ctx context.Context, id int64) error                                                     // ErrNotFound
	CreateLink(ctx context.Context, in model.ManualLink) (model.Link, error)                             // ErrNotFound, ErrConflict
	DeleteLink(ctx context.Context, id int64) error                                                       // ErrNotFound, ErrNotManual

	StartScan(ctx context.Context, target string) (model.Scan, error)
	FinishScan(ctx context.Context, id int64, status, errMsg string) error
	ListScans(ctx context.Context, limit int) ([]model.Scan, error)
	GetScan(ctx context.Context, id int64) (model.Scan, error)                 // ErrNotFound

	ApplyResult(ctx context.Context, scanID int64, r collector.Result) error
	ListChanges(ctx context.Context, limit int) ([]model.Change, error)
	PruneScans(ctx context.Context, keep int) error
}

type DeviceFilter struct {
	Online *bool
	Kind   model.DeviceKind
	Query  string
}
```

`AuthRepo` (the same package) stores users, sessions, tokens and the audit:

```go
type AuthRepo interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, u model.UserRecord) (model.User, error)         // ErrConflict
	GetUserByName(ctx context.Context, username string) (model.UserRecord, error)   // ErrNotFound
	GetUser(ctx context.Context, id int64) (model.User, error)                      // ErrNotFound
	ListUsers(ctx context.Context) ([]model.User, error)
	UpdateUser(ctx context.Context, id int64, role *model.Role, disabled *bool) (model.User, error) // ErrNotFound, ErrLastAdmin
	SetPassword(ctx context.Context, id int64, hash string, mustChange bool) error  // revokes the sessions of the user
	RecordLogin(ctx context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error
	DeleteUser(ctx context.Context, id int64) error                                 // ErrNotFound, ErrLastAdmin

	CreateSession(ctx context.Context, s SessionRecord) error
	GetSession(ctx context.Context, idHash string) (SessionRecord, model.User, error) // ErrNotFound
	TouchSession(ctx context.Context, idHash string, now time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	PruneSessions(ctx context.Context, now time.Time) error

	CreateToken(ctx context.Context, t TokenRecord) (model.APIToken, error)
	GetTokenByHash(ctx context.Context, hash string) (TokenRecord, model.User, error) // ErrNotFound
	ListTokens(ctx context.Context, userID int64) ([]model.APIToken, error)
	RevokeToken(ctx context.Context, id, userID int64) error                         // ErrNotFound
	TouchToken(ctx context.Context, id int64, now time.Time) error

	AddAudit(ctx context.Context, e model.AuditEntry) error
	ListAudit(ctx context.Context, f AuditFilter) ([]model.AuditEntry, error)
	PruneAudit(ctx context.Context, keepDays int) error
}
```

`SessionRecord`, `TokenRecord` and `AuditFilter` are declared in `store` and hold the columns of the matching tables (section 4.1).

Rules:

- The first argument is always `context.Context`. The error "not found" is `store.ErrNotFound`, not `nil` and not a panic. The other errors of the package: `ErrConflict` (a uniqueness violation), `ErrVersionConflict`, `ErrNotManual` (an attempt to delete a link that a scan found), `ErrLastAdmin`.
- `Open(path string)` turns on `journal_mode = WAL`, `foreign_keys = ON`, `busy_timeout = 5000`. For `":memory:"` it sets `SetMaxOpenConns(1)`.
- `ApplyResult` works in one transaction: merging devices by identity, upsert of interfaces, links, services and containers, marking the missing ones (`online = 0`, deleting vanished links, services and containers), writing to `changes`, updating the scan counters.
- `ApplyResult` does not touch devices with `manual = 1` and links with `source = 'manual'` (it neither marks them offline nor deletes them). Vanished links are deleted only with the source `lldp` and `fdb`.
- `ApplyResult` is called only for a scan in which the discovery stage passed. A failed scan changes no state and marks no devices offline.
- `Query` is trimmed at the edges. An empty string after trimming means there is no filter. A match is searched as a substring in `ip`, `mac`, `hostname`, `vendor` without regard to case. The selection is done in Go: `LIKE` in SQLite knows case only for ASCII.
- Sorting by IP is done in Go, by the numeric value of the address.

### 5.2 Collectors

`internal/collector/collector.go`:

```go
type Input struct {
	Targets []netip.Prefix
	Devices []model.Device // live devices after the discovery stage
	Config  config.Config
}

type Result struct {
	Devices    []model.DeviceInput
	Interfaces []model.InterfaceInput
	Links      []model.LinkInput
	Services   []model.ServiceInput
	Containers []model.ContainerInput
}

type Collector interface {
	Name() string
	Collect(ctx context.Context, in Input) (Result, error)
}
```

Rules:

- The order in `scanner`: `arp` (discovery in the L2 segment) → `probe` (routable subnets, if enabled) → `snmparp` (ARP and FDB of routers and switches) → reverse DNS → `snmp` → `lldp` → `services` → `classify` → `ssh` → `docker`. `classify` makes no network requests: it sets `Kind` from the vendor, SNMP data, LLDP capabilities and open ports.
- A collector error after the discovery stage does not fail the scan: the error is written to the log, the other collectors work, and the scan status is `done`. A discovery error gives the status `failed`.
- Merging the results of several collectors by `DeviceKey`: a non-empty value wins over an empty one. On a conflict the source wins by priority `ssh`, `snmp`, `arp`. `Kind = unknown` never overwrites a known kind.
- Each collector gets its dependencies through an interface (`snmpClient`, `dialer`, `sshRunner`, `dockerAPI`), so that tests can substitute fakes.
- Every goroutine ends when `ctx` is cancelled. There are no goroutine leaks.

---

## 6. UI components

They live in `web/src/components/` and are used in all slices. Do not write your own buttons, fields and tables in pages.

The shadcn/ui primitives are added with the CLI command in the slice that uses them: `button`, `card`, `table`, `badge`, `tabs`, `sheet`, `dropdown-menu`, `input`, `skeleton`, `sonner`, `tooltip`, `dialog`, `alert-dialog`, `select`, `textarea`, `label`, `alert`.

Own components:

| component         | props                                                               |
| ----------------- | ------------------------------------------------------------------- |
| `StatusBadge`     | `online: boolean`                                                   |
| `EmptyState`      | `title`, `text`, `action?` (slot)                                   |
| `ScanBar`         | `scan: Scan or null`, `onStart()`, `progress?`                      |
| `DeviceNode`      | a React Flow node, `data: { device, containerCount }`               |
| `TopologyCanvas`  | `topology: Topology`, `onMove(id, x, y)`, `onSelect(id or null)`    |
| `DevicePanel`     | `deviceId: number`, `onClose()`                                     |
| `ExportMenu`      | `formats: ('md', 'html', 'pdf', 'drawio')[]`                        |
| `DeviceForm`      | `device?: Device` (empty: create), `onSubmit(values)`, `onCancel()` |
| `RoleBadge`       | `role: Role`                                                        |
| `UserMenu`        | `user: User`, `onLogout()`                                          |
| `RequirePermission` | `permission: Permission`, `fallback?`, children                   |

There is no separate showcase route. A component that no slice uses is not written.

---

## 7. Routes and API

### 7.1 HTTP API

| method and path                      | what it does                                                      | permission      | response codes         |
| ------------------------------------ | ----------------------------------------------------------------- | --------------- | ---------------------- |
| `GET /healthz`                       | liveness check, no data                                           | public          | 200                    |
| `POST /api/auth/login`               | body `{ "username", "password" }`, sets the session cookie       | public          | 200, 400, 401, 429     |
| `POST /api/auth/logout`              | ends the session                                                  | sign-in only    | 204                    |
| `GET /api/auth/me`                   | the current user, their permissions, the CSRF token               | sign-in only    | 200, 401               |
| `PUT /api/auth/password`             | change your own password (current and new)                        | sign-in only    | 204, 400, 401          |
| `GET /api/topology`                  | devices, links, containers in one answer                          | `topology:read` | 200                    |
| `GET /api/devices?online=&kind=&q=`  | list of devices, the answer is `{ "devices": [...] }`             | `topology:read` | 200, 400               |
| `POST /api/devices`                  | manual add; 409 if the MAC or IP is taken by another record       | `devices:write` | 201, 400, 409          |
| `GET /api/devices/{id}`              | a device, its interfaces and services                             | `topology:read` | 200, 404               |
| `PATCH /api/devices/{id}`            | edit `label`, `notes`, `kind`, the field `version` is required   | `devices:write` | 200, 400, 404, 409     |
| `DELETE /api/devices/{id}`           | delete a device                                                   | `devices:write` | 204, 404               |
| `PUT /api/devices/{id}/position`     | body `{ "x": 1, "y": 2 }`, saves the coordinates                  | `devices:write` | 204, 400, 404          |
| `POST /api/links`                    | a manual link between devices                                     | `devices:write` | 201, 400, 404, 409     |
| `DELETE /api/links/{id}`             | delete a manual link                                              | `devices:write` | 204, 404, 409          |
| `GET /api/containers`                | list of containers                                                | `topology:read` | 200                    |
| `GET /api/services?deviceId=`        | list of services                                                  | `topology:read` | 200, 400               |
| `GET /api/scans`                     | the latest scans                                                  | `topology:read` | 200                    |
| `POST /api/scans`                    | starts a scan                                                     | `scans:run`     | 202, 409               |
| `GET /api/scans/{id}`                | one scan                                                          | `topology:read` | 200, 404               |
| `GET /api/changes?limit=`            | the change feed, newest on top                                    | `topology:read` | 200, 400               |
| `GET /api/events`                    | the SSE stream                                                    | `topology:read` | 200                    |
| `GET /api/export/{format}`           | `md`, `html`, `pdf`, `drawio`, a file with `Content-Disposition`  | `export:run`    | 200, 404, 503          |
| `GET /api/users`                     | list of users                                                     | `users:manage`  | 200                    |
| `POST /api/users`                    | create a user (name, role, temporary password)                    | `users:manage`  | 201, 400, 409          |
| `PATCH /api/users/{id}`              | change the role, disable                                          | `users:manage`  | 200, 400, 404, 409     |
| `PUT /api/users/{id}/password`       | set a new password, turns on `must_change_password`               | `users:manage`  | 204, 400, 404          |
| `DELETE /api/users/{id}`             | delete a user                                                     | `users:manage`  | 204, 404, 409          |
| `GET /api/tokens`                    | your own access tokens                                            | `tokens:manage` | 200                    |
| `POST /api/tokens`                   | issue a token, the value is returned once                         | `tokens:manage` | 201, 400, 409          |
| `DELETE /api/tokens/{id}`            | revoke a token                                                    | `tokens:manage` | 204, 404               |
| `GET /api/audit?limit=&before=`      | the audit log with filters                                        | `audit:read`    | 200, 400               |

SSE events: `scan.started`, `scan.progress` (data `{ scanId, stage, done, total }`), `scan.finished`, `topology.changed`, `config.reloaded`, `config.error`. The `config.*` events go only to users with the permission `users:manage`.

### 7.2 Interface pages

| path            | what it shows                                                   |
| --------------- | --------------------------------------------------------------- |
| `/`             | the topology, the device side panel by `?device=<id>`           |
| `/inventory`    | the table of devices and IPs, search, filters                   |
| `/containers`   | the table of containers                                         |
| `/services`     | the table of services                                           |
| `/changes`      | the change feed and a chart of the number of devices per scan   |
| `/login`        | sign-in                                                         |
| `/account`      | change password, access tokens                                  |
| `/users`        | user management (permission `users:manage`)                     |
| `/audit`        | the audit log (permission `audit:read`)                         |

Rules:

- API responses are JSON, fields in `camelCase`. An error: `{ "error": "text" }` with a fitting code.
- A non-numeric `id` (`/api/devices/abc`) gives 404, not 500 and not 400.
- One scan runs at a time. `POST /api/scans` during a scan answers 409.
- The request body is limited to 1 MiB.
- All paths outside `/api` and `/healthz` serve the embedded `index.html`, and React Router does the routing.
- The `pdf` export without a Chrome found answers 503 with a text on how to get a PDF by printing the HTML.
- Any route except `/healthz` and `POST /api/auth/login` needs a session (cookie) or a token (`Authorization: Bearer`). No authentication - 401, no permission - 403.
- Unsafe methods (`POST`, `PUT`, `PATCH`, `DELETE`) with a session need the header `X-CSRF-Token`, and a token does not need it.
- An access token does not call `/api/users`, `/api/audit` and `/api/auth/*` (except `GET /api/auth/me`).
- The texts of the 401 and 403 answers do not reveal whether a user or a resource exists.
- The "permission" column of table 7.1 is the route table from `internal/api/router.go`. A route without a permission is not registered.

---

## 8. Validation

`config.Validate() error` (returns `errors.Join`) and the checks of API input. Pure functions without network and database.

Config rules:

- Each target in `scan.targets` is a valid CIDR.
- A target is IPv4. IPv6 is not supported in version 1, because discovery goes through ARP.
- The prefix of an IPv4 target is not wider than `/16`.
- A target lies in the ranges of RFC 1918 unless `allowPublicTargets` is on.
- `scan.interval` is `0` or not less than `1m`.
- `snmp.timeout` is from `1s` to `30s`.
- `ssh.hosts[].keyFile` is required for every host.
- `auth.sessionIdleTimeout` is from `15m` to `24h`, `auth.sessionMaxAge` is not less than the idle time and not more than `720h`.
- `auth.minPasswordLength` is not less than 12, `auth.maxFailedLogins` is from 3 to 20.
- The `auth.argon2` parameters are not below `memoryKiB 19456`, `iterations 2`, `parallelism 1`.
- `tls.certFile` and `tls.keyFile` are set together or not at all.
- Each element of `trustedProxies` is a valid CIDR.
- `listen` not on loopback without `tls` and without `trustedProxies` → a warning in the log that the traffic goes unencrypted. The start is not blocked.

API rules:

- The `x` and `y` of a position are finite numbers with an absolute value of at most 100000.
- `limit` is an integer from 1 to 500, 50 by default.
- An unknown export `format` is 404.
- A username matches `^[a-z0-9._-]{3,32}$`, the role is one of `viewer`, `operator`, `admin`.
- A password is from `auth.minPasswordLength` to 128 characters, is not equal to the username and is not in the list of common passwords.
- A manual device: `ip` is empty or a valid address, `mac` is empty or valid (converted to lowercase with colons), at least one of the fields `ip`, `mac`, `label` is not empty, `label` is up to 100 characters, `notes` up to 2000, `hostname` up to 255 characters (optional), `kind` is from the set `DeviceKind`.
- A link: both devices exist and differ, the port is up to 64 characters.
- A token: the name is from 1 to 64 characters, the scope is `read` or `write`, the lifetime is from 1 to 365 days.

Error messages are in English, one line each: `target is not a valid CIDR`, `target is wider than /16`, `target is outside private ranges`, `scan interval is below 1m`, `snmp timeout is out of range`, `ssh host has no key file`, `position is not finite`, `limit is out of range`, `username is invalid`, `password is shorter than the minimum`, `password is longer than 128 characters`, `password equals username`, `password is too common`, `port is longer than 64 characters`, `hostname is longer than 255 characters`, `token name is invalid`, `scope is unknown`, `scope is above your role`, `token lifetime is out of range`, `too many active tokens`, `before is not valid`, `result is unknown`, `from is not a date`, `to is not a date`, `filter is too long`, `version is required`, `role is unknown`, `ip is not valid`, `mac is not valid`, `device has no ip, mac or label`, `label is longer than 100 characters`, `notes are longer than 2000 characters`, `link connects a device to itself`, `session timeout is out of range`, `argon2 parameters are below the minimum`, `tls needs both certFile and keyFile`, `token lifetime is out of range`, `only IPv4 targets are supported`, `min password length is below 12`, `max failed logins is out of range`, `trusted proxy is not a valid CIDR`.

---

## 9. Security (a guide, mandatory for all slices)

The principle: deny by default, minimal rights, the service only reads the network. All parameters below are set in advance. A parameter changes only through `CONTRACT GAP` (section 14). A slice does not weaken a parameter for the convenience of development.

### 9.1 Threat model

What we protect:

- the network map and the inventory (they reveal the topology and the weak points);
- credentials for access to devices (SNMP community, SSH keys);
- accounts, sessions and tokens of the panel;
- the ability to start scans from the host's network;
- access to the Docker socket of the host.

Whom we protect against:

- a network visitor without an account;
- a user with a low role who tries to get higher rights;
- content from the network: hostname, `sysDescr` and container names are controlled by third parties;
- a stolen session or token.

Outside the protection of v1: an attacker with access to the file system of the host (they read the config and the database) and attacks on the network equipment itself.

| threat                    | measure                                                                                                | where it is checked             |
| ------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------- |
| password guessing         | argon2id, account lockout, per-IP attempt limit, the same answer to any login error                    | slice 2                         |
| session theft or fixation | a random identifier, a hash in the database, `HttpOnly`, `SameSite=Strict`, a new identifier at sign-in, revoking sessions | slices 2 and 6 |
| CSRF                      | `SameSite=Strict` and the header `X-CSRF-Token` for unsafe methods                                     | slice 2                         |
| XSS through network data  | escaping on output, CSP without inline scripts, a ban on `dangerouslySetInnerHTML`                     | every slice that shows data     |
| privilege escalation      | a route table with a permission, a check on the server, deny by default                                | slices 2, 3, 5, 6               |
| secret leak               | the type `Secret`, a log filter, secrets only in the config                                            | slices 0, 7, 10                 |
| scanner abuse             | targets from the config, RFC 1918 by default, concurrency limits, the permission `scans:run`, audit    | slices 1 and 3                  |
| service overload          | limits on body and headers, timeouts, SSE and PDF limits                                               | slices 3 and 13                 |
| access through the Docker socket | a read-only socket, `docker-socket-proxy` is recommended                                        | slices 11 and 16                |
| vulnerable dependencies   | `govulncheck`, `npm audit`, Dependabot                                                                 | slice 16                        |

### 9.2 Authentication

| parameter                 | value                                                                                                         |
| ------------------------- | ------------------------------------------------------------------------------------------------------------- |
| password hash             | argon2id, memory 64 MiB, 3 passes, parallelism 2, salt 16 bytes, key 32 bytes, PHC format. The lower bound (OWASP): 19 MiB, 2 passes, parallelism 1 |
| rehashing                 | at a successful sign-in, if the parameters in the hash are weaker than the current ones                       |
| password length           | from 12 to 128 characters, any Unicode characters, no composition rules, not equal to the username, not in the list of common passwords (case-insensitive) |
| username                  | `^[a-z0-9._-]{3,32}$`, case is not distinguished                                                              |
| answer to a login error   | always 401 and the text `invalid username or password`: a wrong password, an unknown user, a locked or disabled account |
| timing equalization       | for an unknown user a check against a dummy hash is performed                                                 |
| lockout at sign-in        | 5 failed sign-ins in a row from one IP under one name lock that pair for 15 minutes, a successful sign-in resets the counter of the pair |
| per-IP limit              | 10 failed sign-in attempts per minute from one IP (a right password does not use up the limit), the answer is 429 with the header `Retry-After` |
| session identifier        | 32 random bytes from `crypto/rand`, base64url, only SHA-256 in the database                                   |
| session cookie            | `netscribe_session`, `HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` over HTTPS                             |
| session lifetime          | 8 hours without activity, 7 days absolute, a new identifier at every sign-in                                  |
| session limit             | 10 per user, the oldest is displaced                                                                          |
| session revocation        | at sign-out, password change, role change, disabling and deleting a user                                      |
| CSRF token                | 32 random bytes per session, returned in `GET /api/auth/me`, sent in `X-CSRF-Token`                           |
| access token              | `ndt_` and 32 random bytes in base64url, SHA-256 and a prefix of 8 characters in the database, the value is shown once |
| token scope               | `read` (viewer rights) or `write` (operator rights), not above the role of the owner, never gives `users:manage` and `audit:read` |
| token lifetime            | 90 days by default, 365 at most, there are no eternal ones, no more than 20 active per user                   |

First start and service rules:

- There is no default password. While the database has no users, all routes except `/healthz` answer 503 with the text `no users: run "netscribe user add"`.
- `netscribe user add <name> --role admin` reads the password from the terminal without echo or from stdin (`--password-stdin`). There is no argument with a password.
- An account with `must_change_password` can only change its password.
- The last administrator cannot be deleted, disabled or demoted.
- Authentication cannot be turned off.

### 9.3 Roles and permissions

| permission      | what it allows                                                                             | viewer | operator | admin |
| --------------- | ------------------------------------------------------------------------------------------ | ------ | -------- | ----- |
| `topology:read` | reading the topology, inventory, services, containers, changes, the event stream           | yes    | yes      | yes   |
| `export:run`    | exporting the documentation                                                                | yes    | yes      | yes   |
| `tokens:manage` | issuing and revoking your own tokens                                                       | yes    | yes      | yes   |
| `scans:run`     | starting a scan                                                                            | no     | yes      | yes   |
| `devices:write` | manual adding, editing and deleting of devices and manual links, node positions on the canvas | no  | yes      | yes   |
| `audit:read`    | reading the audit log                                                                      | no     | no       | yes   |
| `users:manage`  | managing users, the `config.*` events                                                      | no     | no       | yes   |

Rules:

- Only middleware checks permissions, by the route table. Handlers do not compare roles.
- A route without a permission in the table is not registered: registration panics at start. Deny by default.
- The routes `/healthz` and `POST /api/auth/login` are public, the others need a session or a token. The routes `/api/auth/*` except sign-in need only a sign-in, without a permission.
- A user does not change their own role and does not disable themselves.
- The interface hides unavailable actions according to `web/src/lib/permissions.ts`. This is a convenience, and the protection works on the server.
- There are no object permissions: all users see the whole network (section 18).

Multi-user work:

- The topology is shared. Node positions: the last write wins.
- Edits of devices go with optimistic locking by `version`. A mismatch gives 409, and the interface offers to load the current data.
- Every significant action is written to the audit log (9.8).

### 9.4 Transport and headers

- The panel listens on `127.0.0.1:8080` by default. An address beyond loopback without TLS and without a trusted proxy gives a warning in the log and a banner for administrators in the interface.
- TLS: built-in (`tls.certFile`, `tls.keyFile`, TLS 1.2 at minimum) or a reverse proxy. The headers `X-Forwarded-For` and `X-Forwarded-Proto` are accepted only from the addresses in `trustedProxies`, otherwise they are ignored.
- `Strict-Transport-Security: max-age=31536000` is sent only over HTTPS.
- Headers on all responses:
  - `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
  - `X-Frame-Options: DENY`
  - `Permissions-Policy: camera=(), microphone=(), geolocation=()`
  - `Cross-Origin-Opener-Policy: same-origin`
  - `Cache-Control: no-store` for all `/api` paths
- There are no inline scripts. Scripts are included only as external files.
- CORS is not enabled: same-origin only.
- The HTML export is served as an attachment (`Content-Disposition: attachment`) with its own `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; img-src data:` and `X-Content-Type-Options: nosniff`.

### 9.5 Input, output and resources

- Data from the network (hostname, `sysName`, `sysDescr`, container names, images) is untrusted. When written to the database it is cut to 255 characters, and control characters are removed. It is shown only through escaping mechanisms: React, `html/template`, an XML encoder. `dangerouslySetInnerHTML` is forbidden by an eslint rule.
- Queries to the database are only parameterized (sqlc). There is no SQL built from strings.
- The request body is at most 1 MiB, the headers at most 16 KiB.
- Server timeouts: `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s, `WriteTimeout` 60 s, `IdleTimeout` 120 s. SSE and PDF export are excluded from `WriteTimeout`, they have their own deadlines.
- SSE: no more than 5 connections per user. PDF: one render at a time, no longer than 60 seconds.
- File paths from user input are not accepted. The names of exported files are formed by the server.

### 9.6 Scanning

- Targets only from the config. By default only RFC 1918, not wider than `/16`. Public addresses are turned on by the flag `allowPublicTargets`.
- Concurrency limits: 128 ARP warm-up sends, 256 TCP connections per scan, 32 concurrent SNMP requests, 4 SSH hosts. One scan runs at a time.
- Starting a scan needs the permission `scans:run`. The minimum interval of the schedule is 1 minute.
- Adding a device by hand probes nothing.
- Probing routable subnets (`discovery.probeRoutedTargets`) is off by default.
- Slice 18: HTTP reads only the status line and the response headers of `HEAD /`, without a body and without following redirects, with a limit of 8 KiB and 2 seconds per request. Certificate verification is off only for reading headers, and no credentials or cookies are sent. mDNS and SSDP go out only into the local segments of the host, and the address from `LOCATION` in an SSDP answer is never requested. Everything that came over the network is untrusted: it is cut by length and cleaned of control characters.
- SNMP: in v1 only v2c, and the community goes in clear text. The README requires a read-only community and an ACL on the device. The client has no write method, so SNMP SET is impossible.
- SSH: keys only, `known_hosts` is checked strictly, the commands are fixed, a separate account without write rights. The key file permissions are not wider than `0600`, otherwise the host is skipped.
- Docker: the socket is connected read-only. `docker-socket-proxy` allowing only `containers`, `networks`, `events` is recommended. The risk of socket access is described in the README.

### 9.7 Secrets and data on disk

- Secrets (`snmp.communities`, paths to SSH keys) live only in the config. The permissions of the config file are not wider than `0600`, otherwise the service refuses to start. The flag `--allow-loose-permissions` allows a start with a warning for environments such as a bind mount in Docker.
- The database file is created with permissions `0600`, the data directory with `0700`. There is no database encryption in v1, and the protection is at the OS level (section 18).
- Hashes of passwords, tokens and sessions are not returned by the API, not logged and do not get into exports.
- Passwords, tokens, cookie values, the `Authorization` header and communities are forbidden in logs (`slog`). The filter is covered by a test.
- The export contains no secrets, credentials or user data.

### 9.8 Audit

These events are written: `login`, `login_failed`, `logout`, `password_change`, `user_create`, `user_update`, `user_delete`, `user_password_reset`, `token_create`, `token_revoke`, `scan_start`, `device_create`, `device_edit`, `device_delete`, `link_create`, `link_delete`, `export`, `config_reload`, `config_reload_failed`, `access_denied`.

An entry holds the time, the user, the IP, the result (`ok`, `denied`, `error`) and one line of description without secrets. The API cannot change or delete entries: old entries are deleted only by the cleanup by `audit.keepDays` (365 by default). Only the permission `audit:read` reads the log.

### 9.9 Dependencies and delivery

- `govulncheck ./...` and `npm audit --omit=dev --audit-level=high` are run by the command `make audit` in CI. They are not part of the local `make gate`, because they need the network.
- Versions are pinned (`go.sum`, `package-lock.json`). Dependabot watches `go.mod`, npm and the base Docker image.
- Docker image: a minimal final image, `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`. The process does not run as root where possible (access to the socket is solved through `group_add`).
- Releases are published with a checksum file (`checksums.txt`).
- The repository holds `SECURITY.md` with the procedure for reporting vulnerabilities.

---

## 10. Tests

Tests are derived from the acceptance criteria of a slice, not from ready code. The criteria are written in section 16 before the code is written. A test encodes a criterion and does not repeat the implementation.

A forbidden kind of test: "wrote a device, read it, the values matched". Such a test is green on a broken merge by MAC and on duplicates alike.

At least this for every slice:

- a test for every acceptance criterion that has a checkable condition;
- a test for the error path (a target outside the allowed ranges, an SNMP timeout, a missing Docker socket, a non-existent id);
- tests of pure logic (parsers, layout, validation, IP sorting) without network and without a database.

How to build tests:

- Database: `store.Open(":memory:")`, a new one for every test. Tests do not touch the file `netscribe.db`.
- Network: collectors take interfaces (`snmpClient`, `dialer`, `sshRunner`, `dockerAPI`), and tests substitute fakes. SNMP answers are taken from recorded `snmpwalk` output in `testdata/`.
- HTTP: `httptest` on top of `api.NewRouter` with an in-memory database and a fake scanner.
- Time: packages take `clock func() time.Time`, and tests substitute a fixed one.
- Renderers: golden files in `testdata/*.golden` plus structural checks (the XML parses, the number of `mxCell` cells equals the number of devices, containers and links plus two service ones). Golden files are updated with the flag `-update` and checked by eye in review.
- Permissions: one test walks the whole route table and checks three cases for each route: without sign-in - 401, a role without the permission - 403, a role with the permission - neither 401 nor 403. Registering a route without a permission panics, and this is checked too.
- Security: tests for headers (on `/`, `/api/*`, errors and export), for the attributes of the session cookie, and for passwords, hashes, tokens and communities not getting into logs, API answers and exports.
- Isolation and time: a test does not write to package variables that background goroutines read (a substitutable value is passed as a struct field or an argument). A test does not wait "long enough" with `time.Sleep`: it waits for a signal from a channel or polls with a deadline. A pause is allowed only where the test checks that an event did not happen.
- Integration tests against `deploy/compose.lab.yaml` live under the build tag `lab` and are not part of the gate.
- Frontend: vitest for the pure logic from `web/src/lib/`. Components are checked by hand against the criteria of the slice.

---

## 11. Checks and the gate

`Makefile`:

```
make fmt     # gofmt -w, prettier --write
make lint    # gofmt -l (empty output), go vet, golangci-lint, sqlc diff, eslint, prettier --check
make check   # tsc --noEmit
make test    # go test -race ./..., vitest run
make gate    # lint, check, test
make audit   # govulncheck and npm audit, needs the network, runs in CI, not part of gate
make gen     # sqlc generate
make dev     # air (backend) and vite (frontend) in parallel
make build   # build the frontend, then go build into one binary
```

`make gate` red → the slice is not closed. Fixes for the gate are made in the same pass.

What each command catches:

- gofmt and prettier - formatting differences;
- go vet and golangci-lint (including gosec) - unused code, unhandled errors, suspicious constructs, unsafe calls;
- sqlc diff - the generated code fell behind the SQL;
- eslint - dead imports, React hooks with wrong dependencies, a11y errors, `dangerouslySetInnerHTML`;
- govulncheck and npm audit (in CI, by the command `make audit`) - known vulnerabilities of dependencies;
- tsc - types, including a mismatch with the types of section 4;
- go test -race - acceptance criteria and races;
- vitest - the pure logic of the frontend.

---

## 12. Text convention

Code, comments, commits, logs, interface texts, validation messages and the README are in English. This file was written in Russian during the draft and was translated into English in amendment 33.

Commits, Conventional Commits, a fixed format:

```
type(scope): summary
```

- `type` from the set `feat|fix|test|refactor|chore|docs`;
- `scope` from the set `config|store|collector|render|api|web|deploy`;
- `summary` in the imperative, lowercase at the start, no period, up to 50 characters;
- the body only to explain *why*, not *what*;
- commit along the way in small steps, not with one commit at the end. Each commit passes `make check` where possible.

Examples: `feat(collector): add arp warmup sweep`, `test(store): cover mac identity merge`.

Writing rules for all the prose of the project (commits, comments, README): active voice, specifics instead of general phrases, no introductory turns, no long dash, no intensifying adverbs. A comment explains the reason for a decision and does not retell the neighboring line. Do not leave commented-out code.
Before you write the README look at [https://docs.github.com/en](https://docs.github.com/en), [https://github.com/matiassingers/awesome-readme](https://github.com/matiassingers/awesome-readme), [https://www.makeareadme.com/](https://www.makeareadme.com/)

---

## 13. Definition of Done of one slice

1. The acceptance criteria of the slice from section 16 are met and checked by hand (in a browser for the interface, in a terminal for the CLI).
2. Tests are written from the criteria, and `make gate` is green.
3. There are no new files or abstractions beyond those described in this file.
4. There is no dead code: no unused exports, components, styles or SQL queries.
5. The generated sqlc code is current (`sqlc diff` is empty).
6. Commits follow the convention of section 12.
7. Every new route is declared in the route table with a permission, and the parameters of section 9 that the slice touches are met and checked by tests.
8. `tech.md` is not changed (a change of a contract goes separately, through section 14).

---

## 14. CONTRACT GAP

A field, type, function, route or config option is missing - work stops. Issue the block and wait for an answer:

```
CONTRACT GAP
What is needed: <field/type/function/route>
Why: <which acceptance criterion fails without it>
Proposed form: <the exact signature, a column with its type, or a JSON field>
What I do meanwhile: <a local stub inside my slice / wait>
```

Do not write code with an invented type. Do not extend or weaken on your own the database schema, the `model` types, the interfaces `Repo`, `AuthRepo` and `Collector`, the HTTP contract, the config format, the permission matrix and the parameters of section 9.

---

## 15. Rules of behavior in a session

- Think before code: name the assumptions, ask when something is ambiguous, show options instead of a silent choice.
- Simplicity: no features beyond what was asked, no abstractions for one-off code, no handling of errors that cannot happen.
- Surgical edits: do not improve or refactor neighboring working code. Every changed line follows from the current task.
- One slice at a time. Do not roll out the whole application at once.
- Do not run network code against networks that are not yours. All checks go against the lab (`deploy/compose.lab.yaml`), fixtures, or the network of the project owner.
- Do not weaken the security parameters of section 9 for convenience. For development `cmd/seed --dev-user` creates an account `dev` with the role `admin`, the password is taken from the variable `NETSCRIBE_DEV_PASSWORD`, and there is no default password.
- Review goes in a second pass, after the slice is ready, and not in the same message in which the code is written.
- The "Do not" list forbids items in its own slice, not in the whole project: what a later slice implements is named next to it with a link. An item from the list cannot be done "while at it", even if it looks small.

---

## 16. Stages and slices

Status: slices 0-23 are merged into `main`, the plan is complete.

The order is strict, from top to bottom. One slice per session pass. A slice is closed when the Definition of Done from section 13 is met.

A slice is vertical: from a table in the database or a collector to a screen or an export file. Half a slice is not closed.

- **Stage 1 - skeleton.** Slice 0. Features do not start until the criteria of the skeleton are all green.
- **Stage 2 - discovery, access and the first panel.** Slices 1-4: ARP discovery and the CLI, authentication and roles, the API with a device table, the topology on a canvas. Authentication goes before the API: no route appears without a permission check.
- **Stage 3 - manual management and users.** Slices 5-6: manual adding and editing of devices, user management, tokens and the audit log.
- **Stage 4 - enrichment.** Slices 7-11: SNMP and LLDP, extended discovery, services, SSH, Docker containers.
- **Stage 5 - documents.** Slices 12-13: Markdown and Draw.io, HTML and PDF.
- **Stage 6 - operation.** Slices 14-15: change history, the schedule and hot reload of the config.
- **Stage 7 - release.** Slices 16-17: packaging and Docker, interface polish.
- **Stage 8 - recognition.** Slice 18: HTTP headers, mDNS and SSDP. It was added after the first version of the plan and is done after slice 17 by the general rule of order.

---

### Slice 0 - skeleton

**What to build:**

- The Go module `netscribe`, a `Makefile` with the targets of section 11, `.air.toml`, `.golangci.yml`, `sqlc.yaml`, `.gitignore` (`netscribe.db*`, `web/dist`, `web/node_modules`, `tmp`).
- `internal/model/model.go` - the types and constants of section 4.2.
- `internal/config` - YAML loading, `Validate` per section 8, the type `Secret`, `netscribe.example.yaml`.
- `internal/store/store.go` - `Open(path)`, `ErrNotFound`, connecting `goose`. There are no table migrations yet.
- `internal/api/router.go` - the route table `route(method, path, permission, handler)`: `GET /healthz` is marked public, and a route without a permission is not registered. Serving the embedded frontend with a fallback to `index.html`.
- `cmd/netscribe/main.go` - the subcommand `serve` with the flag `--config`.
- `web/` - Vite + React + TypeScript strict + Tailwind + shadcn/ui (initialization), React Router, `web/embed.go`.
- The interface skeleton: a side panel with the name and links to the existing pages, the page `/` with an `EmptyState` "No scans yet".
- A Vite proxy of `/api` and `/healthz` to the backend for development.

**Acceptance criteria:**

1. `make gate` is green on an empty project.
2. `make dev`: editing a `.go` file restarts the server (air), editing a `.tsx` file updates the page without a reload (HMR). Checked by hand.
3. `make build` gives one binary. `./netscribe serve` opens `/` without errors in the console, and `GET /healthz` answers 200.
4. `store.Open(":memory:")` gives a working database without a file on disk.
5. `netscribe.example.yaml` passes `Validate`.

**Tests:** `internal/config/config_test.go` - a valid config, a target that is not a CIDR, a target wider than `/16`, a public target without the flag, a public target with the flag, an interval below `1m`, an interval `0`, an SNMP timeout out of range, an SSH host without a key, `Secret` hides the value in `String()`, JSON and `slog`. `internal/store/store_test.go` - `Open(":memory:")` works. `internal/api/router_test.go` - registering a route without a permission panics, `/healthz` answers 200 without sign-in.

**Do not:** collectors, feature pages, authentication, a Dockerfile, a visual theme.

---

### Slice 1 - ARP discovery and the CLI

**Files:** `internal/store/migrations/0001_devices_scans.sql` (the tables `devices` and `scans`, `devices` without the manual-input columns, section 4.1), `internal/store/queries/*.sql`, `internal/store/apply.go`, `internal/collector/collector.go`, `arp.go`, `scanner.go`, `cmd/netscribe/main.go` (the subcommands `scan`, `devices`).

**What it does:** `netscribe scan` takes the targets from the config, sends a UDP packet to port 9 of every address of the target with a limit of 128 concurrent sends (the kernel performs the ARP request before sending, root rights are not needed), waits 2 seconds and reads `/proc/net/arp`. Entries with the flag `0x2` count as live. For every live address a reverse DNS lookup is done with a timeout of 1 second. `netscribe devices` prints a table: IP, MAC, hostname, online. Discovery works for addresses of the same L2 segment as the netscribe host.

**Acceptance criteria:**

1. `netscribe scan` on a /24 network saves the devices found, and `netscribe devices` shows them.
2. A repeated scan creates no duplicates. A device with the same MAC and a new IP updates the row.
3. A device that is not in the next completed scan gets `online = false` and stays in the database.
4. `netscribe scan 8.8.8.8/32` without `allowPublicTargets` ends with the error `target is outside private ranges`.
5. Ctrl+C stops a scan within 2 seconds, the scan status is `failed`, the error is `canceled`, and the state of the devices did not change.
6. A hostname from reverse DNS gets into the table, and an unavailable DNS does not slow a scan down for longer than the timeout.

**Tests:** a parser of `/proc/net/arp` on a fixture (complete and incomplete entries), `ApplyResult` on an in-memory database (merge by MAC, a change of IP, marking offline, a cancel does not change the state), `scanner` with a fake collector (cancel by `ctx`, a discovery error gives `failed`), a check of a target from an argument.

**Do not:** ICMP (slice 8), SNMP (slice 7), a port scan (slice 9), the HTTP API (slice 3), determining the device kind (slice 8), addresses outside the L2 segment.

---

### Slice 2 - authentication, roles and sessions

**Files:** `internal/store/migrations/NNNN_auth.sql` (the tables `users`, `sessions`, `audit_log`), `internal/auth/` (`password.go`, `session.go`, `rbac.go`, `ratelimit.go`), the implementation of `AuthRepo` in `internal/store/`, `internal/api/router.go` (the permission check), `middleware.go`, `auth.go`, `cmd/netscribe/main.go` (the subcommand `user`), `web/src/pages/Login.tsx`, `Account.tsx`, `web/src/components/UserMenu.tsx`, `RequirePermission.tsx`, `web/src/lib/permissions.ts`, `web/src/api/client.ts` (the CSRF header, handling 401).

**What it does:**

- The subcommands `netscribe user add <name> --role <role>`, `user list`, `user disable <name>`, `user passwd <name>`. The password is read from the terminal without echo or from stdin.
- API `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`, `PUT /api/auth/password`.
- The middleware chain in this order: security headers, body limits and timeouts, authentication (a session, access tokens are added by slice 6), CSRF, the permission check, the handler, audit.
- The permission check by the route table from slice 0 is switched on in middleware: a route with a permission without sign-in gives 401, and with insufficient rights 403.
- Interface: the page `/login`, a redirect to `/login` on a 401 answer, `UserMenu` with sign-out, `/account` with a password change, `RequirePermission`.
- All parameters are taken from section 9.

**Acceptance criteria:**

1. While there are no users, all routes except `/healthz` answer 503 with the text `no users: run "netscribe user add"`.
2. `netscribe user add` creates a user. The password is not accepted as a command-line argument. The database holds an argon2id hash in PHC format.
3. A sign-in with right data answers 200 and sets the cookie `netscribe_session` with `HttpOnly` and `SameSite=Strict`. A wrong password, an unknown user and a locked account give the same 401 answer `invalid username or password`.
4. The fifth failed attempt in a row from one IP under one name locks that pair for 15 minutes (a sign-in with the right password during that time also gives 401), and a successful sign-in resets the counter of the pair. The same user from another IP signs in freely. More than 10 failed attempts per minute from one IP give 429 with `Retry-After`, even if the next password is right. Successful sign-ins do not use up the limit: any number of successful sign-ins in a row from one address gives no 429 and does not reset the failure counter.
5. A request without a session to a protected route gives 401, and so does one with a wrong or expired identifier.
6. An unsafe method with a session and without the right `X-CSRF-Token` gives 403.
7. A session expires after 8 hours without activity and after 7 days absolute. Sign-out and a password change revoke the sessions of the user.
8. All responses (pages, `/api`, errors) carry the headers of section 9.4.
9. A user with `must_change_password` can only change the password.
10. The last administrator cannot be disabled, including by a CLI command.
11. Sign-in, a failed sign-in, sign-out and an access denial (403) are written to `audit_log`.
12. The values of passwords and hashes do not get into logs and answers.

**Tests:** `password` (hash and check, parameters, rehashing), the lifecycle of a session with a substituted clock (idle, absolute lifetime, a new identifier at sign-in), `ratelimit`, the middleware chain through `httptest`, completeness of the route table (see section 10), security headers, CSRF, writing to the audit, no secrets in logs.

**Do not:** OIDC and LDAP, two-factor authentication, self-registration, password recovery by mail, "remember me", roles beyond the three.

---

### Slice 3 - API, SSE and the device table

**Files:** `internal/api/router.go`, `devices.go`, `scans.go`, `sse.go`, `internal/events/hub.go`, `cmd/netscribe/main.go` (`serve` starts the scanner), `web/src/api/client.ts` (extended), `events.ts`, `web/src/pages/Inventory.tsx`, `web/src/lib/` (IP sorting), the components `ScanBar`, `StatusBadge`, `EmptyState`, the primitives `table`, `badge`, `button`, `input`, `skeleton`.

**What it does:** the routes `GET /api/devices`, `GET /api/devices/{id}`, `GET /api/scans`, `POST /api/scans`, `GET /api/scans/{id}`, `GET /api/events`. All routes are declared in the table with the permissions `topology:read` (reading and events) and `scans:run` (`POST /api/scans`). The button that starts a scan is visible only with the permission `scans:run`. The page `/inventory` shows a table of devices with search and an online filter. `ScanBar` starts a scan and shows the progress from SSE. The table refreshes on the event `topology.changed`.

**Acceptance criteria:**

1. `POST /api/scans` answers 202 and starts a scan. A second `POST` during a scan answers 409.
2. `/inventory` shows the devices from the database. After a scan ends the table refreshes without a page reload.
3. Search finds a device by IP, MAC and hostname without regard to case, including Cyrillic names.
4. Sorting by IP is numeric: `192.168.1.9` goes above `192.168.1.10`.
5. `/api/devices/abc` and `/api/devices/999` answer 404.
6. An empty database shows `EmptyState`, and for a role with the permission `scans:run` with a button to start the first scan.
7. A request without sign-in to any route of this slice gives 401. The role `viewer` gets the data (200), but gets 403 on `POST /api/scans` and does not see the start button. The roles `operator` and `admin` start a scan.
8. The stream `GET /api/events` without sign-in gives 401. Scan events reach only signed-in users.

**Tests:** handlers through `httptest` on an in-memory database (criteria 1, 3, 5, 7, 8), `hub` (subscribe, publish, unsubscribe without a goroutine leak), `sortByIp` in vitest (criterion 4).

**Do not:** pagination of the device list (the audit log of slice 6 is paginated), WebSocket, export (slices 12-13), a chart (the chart of the number of devices is in slice 14).

---

### Slice 4 - topology on a canvas

**Files:** `internal/render/layout.go`, `internal/api/topology.go`, `cmd/seed/main.go`, `web/src/pages/Topology.tsx`, the components `TopologyCanvas`, `DeviceNode`, `DevicePanel`, the primitives `sheet`, `tooltip`.

**What it does:** `GET /api/topology` returns devices, links and containers (links and containers are empty for now). The coordinates in the answer are always filled: the saved `x`, `y` stay as they are, and `render.Layout` computes them for the other devices. A layered layout: rows by kind (router, firewall, switch, ap, server, nas, printer, camera, iot, host, unknown), inside a row the order is by IP, the step is 220 by 140, and a row wraps after 8 nodes. The layout is deterministic. The page `/` draws the nodes on React Flow with zoom, pan and a minimap. Dragging saves the position through `PUT /api/devices/{id}/position` (permission `devices:write`). For a role without this permission the nodes cannot be dragged. A click on a node opens `DevicePanel`. `cmd/seed` creates a demo network (a router, 2 switches, 20 hosts, 4 servers) and is extended in slices 7 and 11. The flag `--dev-user` creates an account `dev` with the role `admin` and the password from `NETSCRIBE_DEV_PASSWORD`.

**Acceptance criteria:**

1. `/` draws all devices as nodes with a name, an IP, a kind icon and the state online or offline.
2. A dragged node stays in place after the page reloads.
3. A new device after a scan appears on the canvas. Nodes with saved coordinates do not move. The position of nodes without saved coordinates is computed at every read and may shift: after dragging a node is fixed.
4. A click on a node opens a panel with the device data, and closing and `Esc` hide it. `/?device=<id>` opens the panel on load.
5. On a database of 200 demo devices the canvas stays responsive during pan and drag (a manual check).
6. `PUT` with non-numeric or infinite coordinates answers 400, with a non-existent id it answers 404. The role `viewer` gets 403, and the nodes cannot be dragged for it.

**Tests:** `layout_test.go` (determinism, no coinciding positions, saved coordinates do not change), the `PUT` handler (criterion 6).

**Do not:** an auto-layout button, grouping of nodes, edge routing, manual creation of links (slice 5).

---

### Slice 5 - manual adding and editing of devices and links

**Files:** `internal/store/migrations/NNNN_device_edits.sql` (the columns `manual`, `label`, `notes`, `kind_locked`, `version`, `updated_by`), the `Repo` methods (`CreateDevice`, `UpdateDevice`, `DeleteDevice`, `CreateLink`, `DeleteLink`), `internal/store/apply.go` (the merge rules), `internal/api/devices.go` (`POST`, `PATCH`, `DELETE`), `internal/api/links.go`, `web/src/components/DeviceForm.tsx`, the edit mode in `DevicePanel`, an add dialog on `/inventory` and on the canvas, the primitives `dialog`, `select`, `textarea`, `alert-dialog`.

**What it does:** an operator adds devices that the network does not show (an unmanaged switch, a patch panel, a device on another site), edits `label`, `notes` and the kind (the kind is fixed by the flag `kind_locked`), deletes devices and manual links, and connects nodes on the canvas by dragging with the entry of port names. A manual device without an IP and without a MAC is allowed. Manual data survives scans. A manual device that matches a discovered one by MAC or IP is merged with it into one record (the identity rule of section 4.1).

**Acceptance criteria:**

1. An operator creates a device with a `label` and a kind without an IP and a MAC. It appears in the list and on the canvas and is marked as manual.
2. A scan neither marks a manual device offline nor deletes it.
3. A device created by hand with an IP and a device discovered by a scan with the same IP are merged into one row: the manual fields stay, and the MAC and the scan data are added.
4. An edit of `label`, `notes` and the kind survives a repeated scan. A kind with `kind_locked` is not changed by a scan.
5. A `PATCH` with a stale `version` answers 409 and does not overwrite someone else's edit. The interface offers to load the current data.
6. A manual link is created by dragging between nodes, a scan does not delete it, and an operator deletes it by hand. Deleting a link found by a scan answers 409.
7. The role `viewer` sees no edit buttons, and direct `POST`, `PATCH`, `DELETE` requests from it answer 403.
8. Creating, editing and deleting are written to the audit.
9. Invalid values (incorrect IP and MAC, fields that are too long) are rejected with 400 and the messages of section 8.

**Tests:** merging a manual and a discovered device (3), manual devices do not go offline (2), `kind_locked` (4), a version conflict (5), permissions by role (7), validation (9), audit entries (8).

**Do not:** CSV import, groups and tags, custom fields, an edit history.

---

### Slice 6 - users, access tokens, the audit log

**Files:** `internal/store/migrations/NNNN_api_tokens.sql`, the `AuthRepo` methods for administering users, tokens and audit, `internal/auth/session.go` (tokens), `internal/api/users.go`, `tokens.go`, `audit.go`, `web/src/pages/Users.tsx`, `Audit.tsx`, `Account.tsx` (the tokens section), `web/src/components/RoleBadge.tsx`.

**What it does:** an administrator creates, disables and deletes users, changes roles and sets a temporary password (`must_change_password`). Every user issues and revokes their own access tokens for scripts (`Authorization: Bearer`). An administrator reads the audit log with filters by user, action, result and period. `PruneAudit` is called at start and once a day.

**Acceptance criteria:**

1. An administrator creates a user with a role, the user signs in and sees exactly what their role allows (viewer, operator, admin).
2. A role change, disabling, deleting and a password reset revoke the active sessions of the user.
3. The last administrator cannot be deleted, disabled or demoted (409), including by themselves.
4. An operator and a viewer do not open `/users` and `/audit` and get 403 on the matching routes.
5. A token is shown once at creation, only the prefix is visible in the list, and only the hash is in the database. A token with the scope `read` gets 403 on unsafe methods. A token never opens `users:manage`, `audit:read` and `tokens:manage`, and of `/api/auth/*` only `GET /api/auth/me` is open to it. The rights of a token are the intersection of its scope and the current role of its owner.
6. A revoked and an expired token give 401. The default lifetime is 90 days, the maximum is 365, and an eternal token is not issued.
7. The log shows the events of section 9.8 with filters and pagination through `before`. Entries cannot be changed or deleted through the API.
8. No more than 20 active tokens and 10 sessions per user.
9. Entries older than `audit.keepDays` are deleted.

**Tests:** a role matrix on test data, revoking sessions, the last administrator, tokens (hash, scope, lifetime, revoke), audit filters and pagination, `PruneAudit`.

**Do not:** user groups, custom roles, device-level permissions, a password reset by mail, single sign-on (SSO).

---

### Slice 7 - SNMP, interfaces and LLDP links

**Files:** `internal/store/migrations/0005_interfaces.sql`, `internal/collector/snmp.go`, `lldp.go`, `oui.go`, `oui.csv`, the update of `scanner.go`, `cmd/seed`, `web/src/components/DeviceNode.tsx`, `TopologyCanvas.tsx`, `DevicePanel.tsx`.

**What it does:** for every live device the collector tries SNMP v2c with the community from the config and reads `sysName`, `sysDescr`, `sysServices`, and the tables of interfaces and addresses. The LLDP collector reads `lldpLocPortTable` and `lldpRemTable` and builds links between known devices: the remote node is matched by the MAC from the chassis id or by the management address. An unknown neighbor creates a device with the source `snmp`. The device kind is determined by the LLDP capabilities (bridge → switch, router → router, WLAN access point → ap), otherwise it stays as it was. The vendor is taken from the built-in OUI table by MAC. Links are drawn as edges with port labels.

**Acceptance criteria:**

1. A device that answered over SNMP gets `hostname = sysName`, `description = sysDescr`, the source `snmp`. One that did not answer stays as it is, and the scan does not fail.
2. Interfaces are saved per device, and a repeated scan creates no duplicates.
3. An LLDP neighbor of switch A that is switch B gives one link A.port - B.port, one row and not two.
4. A link with an unknown neighbor creates the neighbor device.
5. A link that vanished from the LLDP table is deleted after the next completed scan.
6. Edges are drawn on the canvas with port labels, and the interfaces tab in `DevicePanel` shows the name, MAC, IP and state.
7. The vendor by OUI is visible in the device panel.
8. The values of communities do not appear in logs, API answers and CLI output.

**Tests:** `snmpClient` with a fake on `snmpwalk` fixtures (criteria 1, 2), normalization and deduplication of a link (`a_device_id < b_device_id`, criterion 3), creating a neighbor (4), deleting a vanished link (5), determining the kind from LLDP capabilities, `Secret` in logs (8).

**Do not:** SNMP v3, SNMP SET, traffic counters, receiving traps, loading MIB files (OIDs are given as numbers).

---

### Slice 8 - extended discovery and classification

**Files:** `internal/collector/snmparp.go`, `probe.go`, `classify.go`, `classify_rules.csv`, the update of `scanner.go`, the config section `discovery`, `cmd/seed`.

**Limits of discovery.** No scanner finds every device. A device that is silent and sends no traffic is visible only as a record added by hand (slice 5).

**What it does:**

- `snmparp`: from known routers and switches that answered over SNMP it reads the ARP table (`ipNetToMediaTable`) and the forwarding table (`dot1dTpFdbTable`, `dot1qTpFdbTable`). This finds devices of other segments and devices that do not answer the ARP warm-up but sent traffic recently. For a host from the FDB a link to the switch port is created (source `fdb`). Ports that have an LLDP neighbor (trunk ports) create no `fdb` links.
- `probe` (only with `discovery.probeRoutedTargets: true`): for targets outside the L2 segment it sends an ICMP echo through an unprivileged ICMP socket (`net.ipv4.ping_group_range` is needed) and does a TCP connect to ports 22, 80, 443. An address that answered any probe, including an RST, counts as live. No more than 128 concurrent probes.
- `classify`: sets the device kind by rules in priority order: LLDP capabilities, SNMP (`sysServices`, keywords of `sysDescr`), open ports (9100, 515, 631 give `printer`, 554 gives `camera`), the vendor by OUI. The rules live in data (`classify_rules.csv`), not in code. With no match it stays `unknown`. A kind with `kind_locked` does not change.

**Acceptance criteria:**

1. A device of another segment that the ARP table of a router knows appears in the inventory with the source `snmp`.
2. A device that did not answer the ARP warm-up but is present in the FDB of a switch appears in the inventory.
3. A host from the FDB gets a link to the switch port. Trunk ports create no links.
4. With `probeRoutedTargets: false` targets outside the segment are not probed, and the reason is written to the log. With `true` live addresses are found by an ICMP or TCP probe.
5. A device with an open port 9100 gets the kind `printer`, with port 554 the kind `camera`. A kind with `kind_locked` does not change.
6. The rules are read from `classify_rules.csv`. A device with no match stays `unknown`.

**Tests:** SNMP fixtures for ARP and FDB (criteria 1-3), excluding trunk ports, `probe` with a fake ICMP and `dialer`, a table of `classify` rules on a test CSV, `kind_locked`.

**Do not:** DHCP leases, mDNS, SSDP and NetBIOS (section 18), scanning behind NAT, a SYN scan on raw sockets, OS detection by stack fingerprint.

---

### Slice 9 - services

**Files:** `internal/store/migrations/0006_services.sql`, `internal/collector/services.go`, `web/src/pages/Services.tsx`, the services tab in `DevicePanel`.

**What it does:** for every live device the collector does a TCP connect scan of the ports from `scan.ports`: a timeout of 500 ms per port, no more than 256 concurrent connections per scan. The service name comes from a built-in table of known ports (IANA). The page `/services` shows the services of all devices with a filter by service name.

**Acceptance criteria:**

1. An open port is saved as a service with a name, and a port that closed is deleted after the next completed scan.
2. A scan takes the ports from `scan.ports`. An empty list turns the stage off.
3. No more than 256 concurrent connections per scan.
4. `/services` shows the name, port, protocol and device. The filter by name works.
5. The services tab in `DevicePanel` shows the services of the selected device.

**Tests:** a fake `dialer` (criteria 1-3, a counter of concurrent connections), the table of port names, the filter on the frontend (vitest).

**Do not:** reading banners and detecting versions (that is slice 18), a UDP scan, vulnerability checks, nmap integration.

---

### Slice 10 - SSH collector

**Files:** `internal/collector/ssh.go`, the update of `scanner.go`, the config section `ssh`.

**What it does:** for the hosts from `ssh.hosts` the collector connects by key and runs a fixed set of read commands: `hostname`, `uname -sr`, `ip -o addr`, `ss -H -tln`. The output fills `hostname`, `description`, interfaces with IPs and listening services. The host key is checked against the file `ssh.knownHosts`. Each command runs with a timeout of 10 seconds, the output is limited to 1 MiB, and the connection has a timeout of 5 seconds.

**Acceptance criteria:**

1. A host from the config is completed: hostname, description (`uname`), interfaces with IPs, listening services. The source is `ssh`.
2. An unknown or changed host key: the host is skipped, the scan continues, and a warning goes to the log.
3. An unreachable host is skipped after the timeout, and the scan does not fail.
4. The collector runs only commands from a constant list. There are no commands from the config or from user input.
5. Key paths and key contents do not get into logs, the API and exports.
6. A key file with permissions wider than `0600` is rejected: the host is skipped with a warning.

**Tests:** a fake `sshRunner` on fixtures (criteria 1, 3), parsers of `ip -o addr` and `ss -H -tln`, the host key policy on an in-memory `known_hosts` (criterion 2), a test that only commands from the list are run (criterion 4).

**Do not:** `sudo` and commands as root, interactive sessions, password sign-in, Windows hosts, parsing the CLI of network equipment, config backup.

---

### Slice 11 - Docker containers

**Files:** `internal/store/migrations/NNNN_containers.sql`, `internal/collector/docker.go`, `internal/api/containers.go`, `web/src/pages/Containers.tsx`, the update of `TopologyCanvas`, `DeviceNode`, `layout.go`, `cmd/seed`.

**What it does:** the collector connects to the socket from `docker.socket`, reads all containers (any state), their ports, networks and the label `com.docker.compose.project`. The container host is the device whose IP equals the local address of netscribe. If it is not in the table, the collector creates it with the source `local`. A subscription to Docker events (`create`, `start`, `stop`, `die`, `destroy`) updates the database between scans and publishes `topology.changed`. On the canvas containers stand under the host node (6 per row), an edge goes from the host to the container, and the color depends on the state. Read-only mode: starting, stopping and logs are not implemented. Container data is served under the permission `topology:read`.

**Acceptance criteria:**

1. With an available socket `/containers` shows the name, image, state, ports, networks and compose project of every container.
2. Starting or stopping a container updates the page within 2 seconds without a scan.
3. The socket is unavailable (no file or no permissions): the service works, the page shows an `EmptyState` with the reason, and the error is written to the log once.
4. Containers on the canvas stand under the host node, and a click opens the container panel.
5. A deleted container disappears from the list and from the canvas.
6. `docker.enabled: false` turns the collector off completely.

**Tests:** a fake `dockerAPI` on fixtures (criteria 1, 5), the event handler (2), a missing socket (3), the mapping of ports and networks to JSON, the layout of containers under a host.

**Do not:** Podman and LXD, remote Docker hosts over TCP, `exec`, logs, container management.

---

### Slice 12 - Markdown and Draw.io export

**Files:** `internal/render/mermaid.go`, `markdown.go`, `drawio.go`, `internal/api/export.go`, `cmd/netscribe/main.go` (the subcommand `export`), `web/src/components/ExportMenu.tsx`, the primitive `dropdown-menu`.

**What it does:** the Markdown holds a title, the generation date, a summary of the counts, the topology in a Mermaid block (`graph LR`), and tables of devices (name, IP, MAC, kind, vendor, state), IPs by interface, services and containers. Draw.io is an uncompressed mxGraph XML: a vertex per device and container with coordinates from `render.Layout`, an edge per link with port labels, and a color by kind. `netscribe export --format md|drawio --out <file>` and `GET /api/export/{format}` give the same result. The CLI reads the local database directly, and the OS checks the rights (access to the database file).

**Acceptance criteria:**

1. `GET /api/export/md` returns Markdown with a `Content-Disposition` of the form `netscribe-<date>.md`. The Mermaid block renders on GitHub (a manual check).
2. `GET /api/export/drawio` returns XML that opens in diagrams.net without errors, and the positions match the panel.
3. The same database state gives the same bytes, except for the line with the generation date (it comes from a substituted clock).
4. An empty database gives a document with the note "no devices", not an error.
5. The characters `<`, `&`, `"`, `|` in a hostname are escaped in XML, in Markdown tables and in Mermaid labels.
6. `netscribe export` and the API give the same result.
7. `GET /api/export/{format}` without the permission `export:run` answers 403, and a successful export is written to the audit. The file is served with `Content-Disposition: attachment`.

**Tests:** golden files on a small fixture, XML parsing and cell counting, escaping tests (criterion 5), determinism (3), an empty database (4).

**Do not:** HTML and PDF (slice 13), custom templates, libraries of equipment icons, Draw.io layers.

---

### Slice 13 - HTML and PDF export

**Files:** `internal/render/svg.go`, `html.go`, `pdf.go`, `templates/report.html.tmpl`, the update of `export.go`, `ExportMenu.tsx`.

**What it does:** the HTML is one self-contained file: built-in styles, a built-in SVG diagram (the same coordinates as in the panel), tables of devices, services, containers and changes, and print styles (`@media print`, page breaks). There are no scripts and no external requests. The PDF is created through `chromedp`, which prints the same HTML. Chrome or Chromium is looked up by `export.chromePath` or in `PATH`.

**Acceptance criteria:**

1. The HTML opens offline, makes no network requests and contains no `<script>`.
2. The SVG shows the same node positions as the panel, and offline devices are dimmed.
3. In the print preview the diagram fits the width and table rows do not break across pages (a manual check).
4. `GET /api/export/pdf` returns a valid PDF (the header `%PDF`) when Chrome is present. Without Chrome it answers 503 with a clear text.
5. A hostname of the form `<script>` is shown as text.
6. The SVG `viewBox` is computed from the bounds of the nodes.
7. The HTML is served as an attachment with the headers of section 9.4 (including its own `Content-Security-Policy`). A PDF render runs at most one at a time and no longer than 60 seconds.

**Tests:** HTML without `<script` (1), escaping (5), the `viewBox` bounds (6), the headers and the limit of PDF rendering (7), the PDF with a skip through `t.Skip` without Chrome and the 503 branch with a fake browser lookup (4).

**Do not:** report themes, a logo and branding, charts in the report, PDF encryption.

---

### Slice 14 - scan history and changes

**Files:** `internal/store/migrations/NNNN_changes.sql`, `internal/store/apply.go` (writing `changes`, `PruneScans`), `internal/api/changes.go`, `web/src/pages/Changes.tsx`, the section "Changes" in the exports.

**What it does:** `ApplyResult` compares a scan result with the previous state and writes the kinds of changes from section 4.1. The first completed scan writes no changes. The page `/changes` shows the feed (newest on top) and a line chart of the number of devices per completed scan (Recharts). `history.keepScans` limits the number of stored scans, and old ones are deleted together with their changes. The exports get a section "Changes" for the last scan.

**Acceptance criteria:**

1. A scan that added, returned, lost or changed a device, a link or a container writes changes with the kinds from the contract. A scan without differences writes nothing.
2. The first completed scan writes no changes.
3. A failed scan writes no changes, marks no devices offline and does not get onto the chart.
4. `/changes` shows the feed and the chart. An empty feed shows an `EmptyState`.
5. Extra scans beyond `history.keepScans` are deleted together with their changes.
6. The Markdown and HTML exports hold a section "Changes" with the changes of the last scan.

**Tests:** one test per kind of change on an in-memory database, the first scan without changes, a failed scan, `PruneScans`.

**Do not:** notifications (email, webhook), comparing arbitrary scans, rolling the state back.

---

### Slice 15 - schedule and hot reload of the config

**Files:** `internal/config/watch.go`, `internal/collector/scanner.go` (the scheduler), `internal/events/hub.go` (the `config.*` events), a toast in `web/src/main.tsx`.

**What it does:** the scheduler starts a scan every `scan.interval`. The config file is watched through `fsnotify` (watching the directory, to catch an atomic replacement of the file by an editor, with a delay of 200 ms) and by `SIGHUP`. A correct config is swapped atomically, and `config.reloaded` is published. An incorrect config is dropped, the old one keeps working, and `config.error` is published with the text of the error. These apply on the fly: `scan.*`, `snmp.*`, `ssh.*`, `docker.*`, `history.*`, `export.*`. The fields `listen` and `database` need a restart, and a warning is written to the log.

**Acceptance criteria:**

1. A change of `scan.interval` in the file applies from the next tick without a restart.
2. A change of targets applies to the next scan, and a running scan finishes with the old settings.
3. Broken YAML or a validation error leaves the old config, the interface shows a toast with the text of the error, and the service keeps working.
4. Saving the file through a replacement (vim, `mv`) is picked up.
5. `SIGHUP` triggers a reload.
6. The scheduler does not start a second scan while one is running, and the tick is skipped with a log entry.
7. A change of `docker.socket` or `docker.enabled` reconnects the Docker collector.
8. The events `config.reloaded` and `config.error` are received only by users with the permission `users:manage`, and the others do not see them. The text of the error contains no secret values. A reload and a failed reload are written to the audit.

**Tests:** the scheduler with a substituted clock (1, 6), a reload of a correct and an incorrect config (2, 3), a file replacement in a temporary directory (4), reconnecting the Docker collector with a fake (7), event delivery by role (8).

**Do not:** a web editor of the config, a per-device schedule, cron expressions, reloading `listen` on the fly.

---

### Slice 16 - packaging and Docker

**Files:** `deploy/Dockerfile`, `deploy/compose.yaml`, `deploy/compose.lab.yaml`, `deploy/lab/`, `.goreleaser.yaml`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `README.md`, `LICENSE`, `SECURITY.md`, `.github/dependabot.yml`, an integration test under the build tag `lab`.

**What it does:**

- `Dockerfile`: a multi-stage build (the frontend, a static Go binary), a minimal final image.
- `compose.yaml`: one service `netscribe`, `network_mode: host`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, `read_only: true`, a volume for the database, the config read-only, the Docker socket read-only (the README describes the risk of socket access and a variant with `docker-socket-proxy`).
- `compose.lab.yaml`: a bridge network with fixed addresses and the containers `snmpd`, `sshd` and `nginx` for checking ARP, SNMP, SSH and services.
- `goreleaser`: static linux/amd64 and linux/arm64 binaries.
- CI: `make gate` and `make audit` on every pull request, a release by tag.
- `SECURITY.md` with the procedure for reporting vulnerabilities and `.github/dependabot.yml` for go, npm and docker.
- The README by the rules of section 12: a description, a screenshot, installation (binary and Docker), creating the first user (`docker compose exec netscribe netscribe user add`), the config, TLS and a reverse proxy, security (the Docker socket risk, SNMP v2c in clear text), export examples.

**Acceptance criteria:**

1. `docker compose -f deploy/compose.yaml up --build` from a clean clone brings the service up, and `GET /healthz` answers 200. The other routes answer 503 until `docker compose exec netscribe netscribe user add` creates the first user.
2. `compose.lab.yaml` brings the lab up, and a scan finds its containers, the SNMP data from `snmpd`, and the services from `nginx` and `sshd`.
3. `goreleaser build --snapshot --clean` gives linux/amd64 and linux/arm64 binaries, and `ldd` reports that the file is not dynamic.
4. CI runs `make gate` and `make audit` on a pull request.
5. The README has the sections: what it is, installation, the first user, configuration, security, export. The repository has `SECURITY.md` and `.github/dependabot.yml`.

**Tests:** an integration test under the tag `lab` runs a scan against the lab and checks the number of devices and services found. It is not part of the gate.

**Do not:** Helm and Kubernetes, builds for Windows and macOS, auto-update, telemetry.

---

### Slice 17 - interface polish

**Files:** `web/src/` (the theme, loading and error states), `index.html`, `web/public/theme-init.js`, `web/src/lib/theme.ts`, the eslint config (`jsx-a11y`).

**What it does:** a light, a dark and a system theme: a class on `<html>` and a value in `localStorage`, and an external synchronous script `web/public/theme-init.js` in `<head>` sets the class before the first paint (inline scripts are forbidden by the CSP of section 9.4). Loading states (`skeleton`), error states (an error boundary with a retry button) and empty states exist on every page. The key `/` focuses the search on the inventory page. Focus rings, `aria` labels on nodes and buttons. The side panel collapses on a narrow screen.

**Acceptance criteria:**

1. The chosen theme applies without a flash when the page reloads.
2. The "system" mode follows the OS setting and reacts to its change.
3. Every page has loading, empty and error states.
4. The key `/` on `/inventory` focuses the search field.
5. `eslint-plugin-jsx-a11y` passes without errors.
6. The interface is usable at a width of 1280 and 390 pixels (a manual check).
7. There are no inline scripts in `index.html` and in the built pages, and the browser console shows no CSP violations.

**Tests:** the theme detection function (`theme.ts`) in vitest.

**Do not:** i18n, branding, animations beyond simple transitions.

---

### Slice 18 - recognition by HTTP, mDNS and SSDP

**Files:** `internal/store/migrations/NNNN_service_banner.sql` (the column `services.banner`), `internal/collector/httpbanner.go`, `mdns.go`, `ssdp.go`, the update of `classify.go`, `classify_rules.csv`, `scanner.go`, `web/src/components/ServicesTable.tsx`, `web/src/pages/Services.tsx`.

**What it does:** three sources of information about what kind of device it is, for devices that are silent over SNMP and LLDP.

- **HTTP.** For every device with an open port 80, 443 or 8080 from the result of slice 9, one `HEAD /` request. From the answer it takes `Server`, `X-Powered-By` and the `realm` from `WWW-Authenticate`. The line is saved in `services.banner` (up to 200 characters). For HTTPS it reads the same, plus the CN of the certificate.
- **mDNS.** One PTR query for `_services._dns-sd._udp.local` to `224.0.0.251:5353` from every local interface, with an answer window of 3 seconds. From the answers it takes the host name (`.local`) and the service types (`_ipp._tcp`, `_airplay._tcp` and others). The host name fills `hostname` only if it is empty.
- **SSDP.** One `M-SEARCH` with `ST: ssdp:all` to `239.255.255.250:1900`, a window of 3 seconds. From the answers it takes `SERVER` and `ST`. The address from `LOCATION` is not requested.
- **Classification.** The fields `http` (a substring of the banner), `mdns` (a service type) and `ssdp` (a substring of `SERVER` or `ST`) appear in `classify_rules.csv`. The field order is `descr`, `http`, `mdns`, `ssdp`, `services`, `port`, `vendor`. The rules are in data, not in code.

**Acceptance criteria:**

1. A device with a web port gets the value of `Server` in `services.banner`, not longer than 200 characters, without control characters.
2. The HTTP collector reads only headers: the response body is not read, redirects are not followed, and a request takes no more than 2 seconds and 8 KiB.
3. A device that is silent over SNMP but announces the service `_ipp._tcp` in mDNS gets the kind `printer`. A kind from LLDP and `kind_locked` do not change.
4. A `.local` name from mDNS fills an empty `hostname` and does not replace one already known.
5. The address from `LOCATION` in an SSDP answer is not requested anywhere.
6. `discovery.httpHeaders`, `discovery.mdns` and `discovery.ssdp` are switched on separately, and a source that is off sends no packet.
7. An answer longer than the limit, a broken packet and an answer with garbage instead of UTF-8 are skipped without the scan failing.
8. The page `/services` and the services tab show the banner.

**Tests:** `httptest` for HTTP and HTTPS (criteria 1, 2), parsing mDNS packets and SSDP answers on bytes assembled in the test (criteria 3-5, 7), a fake multicast transport (criterion 6), classification rules on a test CSV, the banner filter on the frontend (vitest).

**Do not:** requests with credentials, a `GET` with a body, following redirects, loading `LOCATION` and UPnP device descriptions, passive listening, NetBIOS, version detection by fingerprints, vulnerability checks.

---

### Slice 19 - Nmap: OS detection and control from the panel

A refinement of the optional Nmap source (amendment 20). The source is still off by default and runs only for devices that the other sources did not identify.

Files: `internal/collector/nmap.go`, `internal/store/migrations/0010_device_os_guess.sql`, `internal/api/scans.go`, `internal/api/router.go`, `web/src/components/ScanBar.tsx`, `web/src/pages/Inventory.tsx`, `web/src/components/DevicePanel.tsx`, `deploy/Dockerfile` (the target `nmap`), `deploy/compose.nmap.yaml`.

- The OS name from the best Nmap guess (accuracy of at least 85 percent) is saved in `devices.os_guess` (up to 100 characters) and shown in the device panel as "System (Nmap)". A scan without Nmap or without a guess does not erase the old value. The source `nmap` does not change the source, the name or the description of a device.
- `POST /api/scans` accepts an optional body `{"nmap": true}`: this scan runs with Nmap whatever `discovery.nmap` says. The answer is 400 if Nmap is not found or the scanner does not support it. In the audit the entry `scan_start` gets the mark `with nmap`.
- `GET /api/nmap` (permission `scans:run`) returns `{available, enabled, reason}`. The panel shows the checkbox "Identify unknown devices with Nmap" next to "Run scan" only if Nmap is found; with `discovery.nmap: true` the checkbox is on and locked.
- A Docker image with Nmap: the target `nmap` in `deploy/Dockerfile` (Alpine, `setcap` on `nmap`), and the file `deploy/compose.nmap.yaml` gives the container `NET_RAW`, `NET_ADMIN`, `NET_BIND_SERVICE` back and does not set `no-new-privileges`, because the kernel does not apply file capabilities with that option. The default image stays distroless without Nmap.

Acceptance criteria.
1. A scan with `--nmap` or with the checkbox saves the OS guess for a device without a kind, and the panel shows it.
2. Without Nmap or without rights the scan finishes, the log has one warning, and the checkbox is hidden.
3. A real Nmap is checked in CI on the lab (`NETSCRIBE_LAB_NMAP=1 go test -tags lab`) and in the `nmap` image under the user `nonroot`.

### Slice 20 - Nmap and the discovery logic

Brings Nmap and discovery to a working state after slice 19 was checked against a real Nmap. The source is still off by default.

Files: `internal/collector/nmap.go`, `internal/collector/classify.go`, `internal/collector/classify_rules.csv`, `internal/collector/scanner.go`, `internal/config/config.go`, `internal/store/manual.go`, `internal/api/devices.go`, `internal/api/router.go`, `cmd/netscribe/main.go`, `web/src/components/DevicePanel.tsx`, `web/src/api/client.ts`.

- **Only devices with an open port.** OS detection needs one open and one closed TCP port. Nmap runs only for devices for which the port scanner or the probe found at least one open TCP port. It does not run for the others, so as not to wait for the device timeout (up to 90 seconds).
- **The OS family as a fact.** From `osclass` the `osfamily` is taken and a fact `family=<family in lowercase>` is added. In `classify_rules.csv` the rules `nmap,family=windows,host`, `family=mac os x` (the name Nmap gives macOS) and `family=android` give the kind `host`. Nmap uses one name, `iOS`, for both Apple iOS and Cisco IOS, so the Apple family is told apart by the vendor of the class and written as `family=apple ios` (the rule gives `host`), while Cisco stays `family=ios` without a rule. The Linux family sets no kind. The `type=` rules stand earlier in the file than the `family=` rules and win.
- **Scope.** The key `discovery.nmapScope`: `untyped` (the default) asks only about devices without a kind, `all` asks about all devices with an open port, to fill the OS line for typed ones too. Another value fails config validation.
- **One device from the panel.** `POST /api/devices/{id}/identify` (permission `scans:run`) runs Nmap for one device in the background and answers 202. The answer is 404 if there is no such device, 409 if a scan or another identification is running, 400 if Nmap is unavailable or the device has no IP. The result: the OS line is written to `devices.os_guess`, and the kind changes only if it is `unknown` and not locked by hand (`kind_locked`). Audit `device_identify`, and when it finishes the event `topology.changed`. In the device panel the button "Identify with Nmap" is visible when Nmap is found and the user has the permission `scans:run`.

Acceptance criteria.
1. A device without open ports in a scan with Nmap does not cause Nmap to run.
2. A device with a guess of the Windows family gets the kind `host`, and one with the type `router` gets `router`, even if the family is named too.
3. `nmapScope: all` fills the OS line of a device whose kind is already known, and `untyped` does not.
4. The button in the panel identifies the OS of one device, does not touch a kind locked by hand and writes an audit entry.

### Slice 21 - code review, port and installation

Fixes the findings of the review and brings startup and delivery to a state in which Netscribe installs on any Linux distribution.

Files: `cmd/netscribe/main.go`, `cmd/netscribe/listen.go`, `internal/config/config.go`, `internal/store/queries/scans.sql`, `internal/store/devices.go`, `internal/auth/session.go`, `internal/api/sse.go`, `internal/api/middleware.go`, `scripts/`, `deploy/netscribe.service`, `AGENTS.md`, `CONTRIBUTING.md`, `docs/`, `README.md`, `.goreleaser.yaml`.

- **Port.** `serve` takes the port before starting any workers. If the port is busy, the server warns and on a terminal asks for another port (`auto` takes the next free one), with the flag `--auto-port` it takes the next free one without asking, and without a terminal it exits with a clear error. The flag `--listen` replaces `listen` from the config. The chosen port is not written to the config.
- **`listen` in `Validate`.** `host:port` is required, with a port from 1 to 65535.
- **Unfinished scans.** At start `serve` closes scans in the state `running` as `failed`: such a scan remains only after the process was stopped.
- **Event stream.** Every 25 seconds the stream checks the credentials again (`Service.Recheck` does not renew the idle timer). A sign-out, a disabled account, a password change and a revoked token end the stream.
- **Installation.** `scripts/install.sh` installs the release, the user `netscribe`, the config and the systemd unit, and when needed Nmap and Chromium through the package manager (apt, dnf, yum, zypper, pacman, apk, xbps). `scripts/install-build-deps.sh` installs git, make, Go and Node.js for building from source. The unit is part of the release archive.

Acceptance criteria.
1. A busy port without a terminal and without `--auto-port` ends `serve` with an error, the database is not open and no workers are started.
2. With `--auto-port` the server listens on the next free port, on a terminal the entered port or `auto` gives a working listener, and an empty answer ends the command.
3. `listen: 8080` and port 0 or 65536 are rejected when the config loads.
4. A scan left in `running` has the status `failed` after `serve` starts, and completed scans do not change.
5. The event stream closes within one interval after an account is disabled and stays open while it is enabled.
6. The installer rejects an archive with a wrong sum, does not overwrite an existing config and installs the config with permissions 0600.

### Slice 22 - the installer with a setup wizard

A second installer next to the classic `install.sh`: the `netscribe` command, in which installation, configuration, recommendations and checks go item by item.

Files: `scripts/get.sh`, `scripts/install.sh`, `cmd/netscribe/setup.go`, `cmd/netscribe/main.go`, `internal/setup/`, `docs/installation.md`, `README.md`, `AGENTS.md`.

- **`scripts/get.sh`.** Installs the release through `install.sh --no-service` (the checksum is checked there), then does `exec netscribe setup`. The options `--version`, `--prefix` and `--base-url` go to `install.sh`, the others to `setup`. Run through `curl | sh`, it reads the answers from `/dev/tty` unless `--yes` or `--password-stdin` is given.
- **`netscribe setup`.** Nine steps: the system, the `netscribe` command, the service account, features (Nmap, Chromium, Docker), the config, ICMP, the first administrator, the service, the result. Each step prints its findings with a level and a fix. Flags: `--config`, `--yes`, `--dry-run`, `--no-service`, `--admin`, `--password-stdin`, `--targets`, `--listen`.
- **Config.** An existing config is not overwritten. An invalid one stops the installation. A new one holds only `listen`, `database`, `scan.targets`, `scan.interval`, `docker`, `discovery.nmap`, the rest is default, mode 0600, the owner is the service account, and the write is atomic. Targets are taken from the interfaces: only private IPv4, a network wider than /24 is narrowed to the /24 of the address, and interfaces of containers and virtual machines are skipped.
- **Service rights.** The group of the Docker socket and the Nmap rights (`CAP_NET_RAW`, `CAP_NET_ADMIN`) go into the drop-in `netscribe.service.d/10-setup.conf`, and the main unit is not edited. `net.ipv4.ping_group_range` is changed only when ICMP is off for all groups (`1 0`): the wizard does not replace someone else's non-empty range.
- **First administrator.** The wizard runs `netscribe user add` as the service account, otherwise the database would belong to root. The password is typed into the command itself, and the wizard never sees it. On a weak password it asks whether to try again.
- **Address choice.** `127.0.0.1:port` (recommended), an interface address, `0.0.0.0:port`, your own address. Beyond loopback the protection is asked: a proxy (`trustedProxies`, in CIDR form: a bare address gets `/32`), TLS (the certificate and key are checked to be a pair and written to `tls`), or plain HTTP after an explicit "yes". In flag mode the same is set by `--tls-cert/--tls-key`, `--trusted-proxy` and `--plain-http`; without them an address beyond loopback is rejected.
- **Console.** `netscribe` without arguments in a terminal, and `netscribe console`: Watch, Check, Scan (the networks of the config, with Nmap, or networks you name), Devices, Export (Markdown, HTML, PDF, Draw.io into a file that belongs to the person behind sudo), Settings, Accounts (the `user` commands as the service account), Service (status, start, stop, restart, reload by SIGHUP, start at boot, log), Setup again, and Commands (the list of commands). Without a terminal the bare command prints the usage and exits with 2, as before. **A way out and a yes.** `q`, `quit`, `exit` or the end of input leave the program from any question of the console, the wizard and the settings, and `b` goes back from any submenu that has a Back item. Every action that changes something asks first and names what it will do: each change of the wizard (the account, the directories, the config, each unit file), starting a scan, writing an export (and replacing an existing file), start, stop, restart and reload of the service, start at boot, adding, resetting and disabling an account, saving a setting (with the old and the new value), the SSH hosts, restoring the config and running the setup again. A no changes nothing; in the wizard it stops the run before that change, with the step named and the words that what was done stays done. Reading asks nothing. `--yes` and `--dry-run` do not ask, as before.
- **`netscribe watch`.** Once per `--interval` (2 s, not less than 200 ms) it takes the state: `systemctl show`, `/proc/PID` (memory, threads, files, CPU from the difference of ticks), `/healthz` with the latency and a chart, the size of the database and of the WAL journal, free space, the load and memory of the host, the validity and time of the config, the last lines of the journal. It shows the events between samples (the service fell or restarted, the panel stopped answering, the config does not load, low space, memory growth) and rings the terminal bell on a bad event. `--once` prints one sample and exits with 1 if the service is not running.
- **`netscribe config`.** Groups: panel access, scanning, discovery, Docker, SNMP, history and audit, sign-in policy, export, SSH. Every setting has a value, an explanation, a default value (checked by a test against `internal/config`) and a mark of whether it applies without a restart (like `mergeHot`). SSH hosts are added and removed in their own submenu. A write: an edit of the YAML tree that keeps the comments, a check of the whole config by `config.Load` on a temporary file, a copy `.bak` (0600), and an atomic replacement with the previous owner. "Restore the previous config" swaps the file and its `.bak` through the same check. SNMP communities are typed without echo and are not printed; they cannot be set through command-line arguments. Subcommands: `show` (secrets hidden), `get`, `set`, `edit` (the editor from `$VISUAL`/`$EDITOR`, the result is checked before saving).
- **`netscribe doctor`.** The same checks, changes nothing, exit code 1 on `Fail`.
- **Checks.** The platform, root, systemd, disk space, the `netscribe` command, the account, the state directory, the validity of the config and the owner of the file, a listen address without TLS, a busy port, targets outside the host's networks, ICMP, Nmap and its rights, a browser for PDF, the Docker socket and group, the SNMP community `public`, the unit, the state of the service, the answer of `/healthz`, the presence of accounts.

Do not: edit the main unit and someone else's config, change `ping_group_range` when other groups use it, put a password into process arguments, TLS certificates (issuing them stays with the owner).

Acceptance criteria.
1. `setup --dry-run` changes nothing: no files, no accounts, no services.
2. `setup --yes --admin alice --password-stdin` on a clean machine gives a working panel, a config 0600 owned by the service account, a unit with the paths of this installation and the administrator alice.
3. A repeated run changes nothing: the config, the unit and the account stay, and the administrator is not created a second time.
4. An existing config is not overwritten, and an invalid one stops the installation with a message.
5. Without root and without a terminal (and without `--yes`) `setup` ends with a clear error.
6. The container network does not get into the proposed targets, and a network wider than /24 is narrowed to /24.
7. `doctor` on a broken installation shows a fix for every finding of level `Warn` and `Fail` and exits with code 1 on `Fail`.
8. The copy of the unit in `internal/setup` equals `deploy/netscribe.service`.
9. Address choice: loopback by default, interfaces without container bridges, and for an address beyond loopback without TLS and a proxy in flag mode the installation refuses; a proxy is written as a CIDR.
10. `config set` with an invalid value changes nothing and leaves no `.bak`; a successful edit keeps the comments and the mode 0600 and makes a copy.
11. `watch --once` exits with 1 when the service is not running or the panel does not answer; the events list the fall, the restart, the loss of the answer and the corruption of the config.
12. Secrets (SNMP communities) get into neither the output of `config show/get`, nor process arguments, nor the log.
13. `q`, `quit` and `exit` stop the wizard at any question with the step named, and nothing after that step is done. A no to the confirmation of an account, a directory, the config or a unit file stops the wizard before that change.
14. In the console every submenu with a Back item leaves on `b` without running anything, and `q` leaves the whole console from inside a submenu.
15. Scan, export, service start/stop/restart/reload/boot, account add/reset/disable, saving a setting, SSH host add/remove, restoring the config and running the setup again do nothing after a no, and ask before they do anything after a yes. An existing export file is replaced only after a separate yes.
16. Restoring the previous config puts the replaced config in `.bak`, and without a `.bak` it reports that there is no previous config.

### Slice 23 - interface design

The goal: the interface looks like an expensive control console, not like a set of components from a library. The reference was set by the owner: dark minimalism, thin borders, depth, a dosed blue accent, an atmospheric network of dots in the background.

Files: `web/src/index.css`, `web/src/lib/particles.ts`, `web/src/lib/spotlight.ts`, `web/src/components/AmbientBackground.tsx`, `web/src/components/Brand.tsx`, `web/src/App.tsx`, `web/src/pages/Login.tsx`, the primitives in `web/src/components/ui/`, `web/public/theme-init.js`, `web/src/lib/theme.ts`.

- **Tokens.** A role, not a color: `background`, `card`, `popover`, `muted`, `accent`, `primary`, `brand`, `border`, `input`, `ring`, `sidebar`, `glow`. A component names a role (`bg-card`, `text-muted-foreground`), and no color is written in components. Dark theme: the background `#060607`, cards `#111113`, lines in white at 8 percent, the main text `#f4f4f5`, the second text `#8d9098` (contrast on a card at least 5.9). The accent `#4059f5` for buttons (white text, contrast 5.3) and `#8095ff` for icons and text on dark (contrast 6.7). The light theme has the same structure.
- **Surface.** The class `.surface`: a matte gradient, a light line on the top edge, a multilayer soft shadow, the radius 2xl, and under the cursor a light spot of the accent (`--mx`, `--my` are written by `spotlight.ts` once per frame by one listener per page). `.surface-lift` raises a card by 2 pixels. Cards, tables, the scan and link bars, the topology nodes and the topology canvas are made with it. Menus, dialogs, tooltips and sheets use `popover`.
- **Frame.** A permanent sidebar with the brand, the groups "Workspace" and "Administration", an item with an indicator and a glow, and a user card with an avatar at the bottom. The top bar: on a narrow screen a round menu button and the brand, on a wide one a capsule with the page title, and on the right a round theme button. The sign-in page: the brand and a card in the center.
- **Background.** `AmbientBackground`: a canvas over the whole window, up to 110 dots (the number depends on the area), about half are born in groups that drift together, thin lines between close dots, twinkling, three light pulses along the lines, a reaction to the cursor (a glow and lines to the nearest dots, a shift by depth). The brightness is low, it is drawn behind the content, with `aria-hidden` and `pointer-events: none`, it stops in a hidden tab, and under `prefers-reduced-motion` it draws one frame. The dot computation is moved into `particles.ts` and checked by tests without a canvas. Two soft spots of light behind everything drift by a CSS animation on the compositor.
- **Motion.** Page appearance: the page fades in and its sections rise one after another (a delay of 70 ms per section, 700 ms for the rise, a quint curve). Micro-interactions of 150-300 ms: buttons, menu items, icons, table rows, menus and tooltips (a short appearance). `transform` and `opacity` are animated, and there is no layout recalculation.
- **The default theme is dark** (see amendment 32).

Do not: WebGL and three.js (canvas 2D is enough, and the dependency and the risk for weak devices are unneeded), loud neon, fully glass surfaces, emoji instead of icons, an animation that gives no feedback.

Acceptance criteria.
1. No component writes a color: colors only through tokens, both themes are readable, and the contrast of the main and the second text is at least 4.5.
2. There are no external requests: fonts and scripts come from the same origin, the CSP did not change, and the browser console has no errors on all pages.
3. Under `prefers-reduced-motion: reduce` there is neither dot drift nor section appearance.
4. At 1440, 1024 and 390 pixels there is no horizontal page scroll, text is not cut off and elements do not overlap (tables scroll inside their own card).
5. The background does not interfere with reading: the brightness of dots and lines is low, and the content is always on top.
6. All existing page tests pass without changing their expectations, except for the themes (`dark` by default).

---

## 17. Review after each slice

A separate pass, after the slice is ready and the gate is green. The task of the pass is to look for problems, not to praise what was written.

Checklist:

1. SQL only in `internal/store/migrations` and `internal/store/queries`. There are no SQL strings in other packages.
2. `collector` does not import `store`, `render` does not import `collector` and `api`, components do not call `fetch`.
3. The names of tables, fields, types and routes match sections 4, 5 and 7.
4. There are no database queries in a loop. A list and the related data come in one query or one transaction.
5. Every goroutine ends on `ctx`. There are no goroutines without an exit and no writes to a closed channel.
6. Errors are returned or logged, and none is ignored silently (`_ =` without a reason in a comment).
7. Secrets do not get into logs, API answers, exports and error messages.
8. Network code respects the security boundaries of sections 1 and 9.6: private ranges, read-only mode, fixed SSH commands.
9. Tests check the acceptance criteria and do not repeat the implementation. For every criterion with a refusal there is a test of the refusal.
10. The components of section 6 are used, and there are no self-made buttons and fields in pages.
11. There is no dead code: unused exports, SQL queries, components, styles.
12. No files and abstractions beyond section 3 appeared.
13. Every new route is declared in the route table with a permission, there is no direct registration, and there are tests for 401 and 403.
14. Data from the network (hostname, `sysDescr`, container names) is normalized when written and escaped when shown, and `dangerouslySetInnerHTML` is not used.
15. The parameters of section 9 (cookies, headers, limits, timeouts, session and token lifetimes) are not weakened.

The findings are fixed in the same pass, then the gate is run again.

---

## 18. Open questions (delete after finalization)

1. ~~Project name~~ Closed: Netscribe.
2. **Interface language.** In the draft the interface is in English without i18n. An alternative: Russian and English at once through i18n.
3. **Topology layout.** In the draft there is one deterministic layered layout in Go, used by the panel and all exports. An alternative: `elkjs` in the interface for a nice auto-layout on a button, and then the coordinates are saved in the database and the exports take them from there.
4. **PDF.** In the draft through `chromedp` and an external Chrome. An alternative: only print styles in the HTML, with no dependency on a browser. If Chrome is needed in the Docker image, the image becomes noticeably larger, and a separate tag is possible.
5. **License.** MIT, Apache-2.0 or AGPL. It affects the repository and the README.
6. **SNMP v3.** In the draft only v2c, and the community goes in clear text (section 9.6). SNMP v3 adds authentication and encryption but complicates the config and the storage of secrets. For sensitive networks v3 is worth including in v1.
7. **External authentication and a second factor.** In the draft only local users with a password. Options: OIDC (single sign-on), LDAP, TOTP as a second factor.
8. ~~Password check~~ Closed: length, a ban on equality with the username and a built-in list of common passwords (amendment 27).
9. **Encryption of data at rest.** The database and the secrets of the config are not encrypted, and the protection is file permissions. Options: SQLCipher, encrypting secrets with a master key.
10. **Object permissions.** All users see the whole network. An option: access by segments or groups of devices.
11. **Discovery sources.** Not included: DHCP leases (they depend on the router model), passive listening (slice 18 has only one active mDNS and SSDP query per scan), NetBIOS, importing devices from CSV.
12. **Forgetting devices.** Offline devices stay forever. An option: `history.forgetAfterDays`.
13. **Charts over time.** In v1 the number of devices per scan. Interface traffic (SNMP counters) and uptime are left outside v1.
14. **PostgreSQL.** In v1 only SQLite. The interfaces `Repo` and `AuthRepo` leave room for a second backend.
15. **Platforms.** In v1 only Linux.
16. **Release signing.** In v1 a checksum file. An option: a `cosign` signature and an SBOM.
